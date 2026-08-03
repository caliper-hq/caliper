import { Injectable } from '@nestjs/common';
import { InjectRepository } from '@nestjs/typeorm';
import { Repository } from 'typeorm';
import { HistoricalRun } from './historical-run.entity';
import { RunDto } from './run.dto';
import * as fs from 'fs';
import * as path from 'path';

@Injectable()
export class RunsService {
  constructor(@InjectRepository(HistoricalRun) private readonly runs: Repository<HistoricalRun>) {}

  async ingest(projectId: string, payload: RunDto[]): Promise<{ accepted: number; duplicates: number }> {
    let accepted = 0; let duplicates = 0;
    for (const run of payload) {
      const exists = await this.runs.existsBy({ projectId, runId: run.run_id });
      if (exists) { duplicates++; continue; }
      try {
        await this.runs.insert({
          projectId,
          runId: run.run_id,
          datasetId: run.dataset_id,
          configVersion: run.config_version,
          timestamp: new Date(run.timestamp),
          avgLatencyMS: String(run.telemetry.avg_latency_ms),
          costUSD: run.telemetry.cost_usd,
          overallScore: run.telemetry.overall_score,
          passed: run.telemetry.passed,
          results: (run.results ?? []) as any,
        });
        accepted++;
      } catch (error: unknown) {
        if ((error as { code?: string }).code === '23505') duplicates++; else throw error;
      }
    }
    return { accepted, duplicates };
  }

  async metrics(projectId: string): Promise<{ from: string; avg_ttft_ms: number; avg_cost_usd: number; run_count: number }> {
    const since = new Date(); since.setUTCDate(since.getUTCDate() - 30);
    const row = await this.runs.createQueryBuilder('run')
      .select('COUNT(*)', 'count')
      .addSelect('COALESCE(AVG(run.avg_latency_ms), 0)', 'latency')
      .addSelect('COALESCE(AVG(run.cost_usd), 0)', 'cost')
      .where('run.project_id = :projectId AND run.timestamp >= :since', { projectId, since })
      .getRawOne<{ count: string; latency: string; cost: string }>();
    return { from: since.toISOString(), avg_ttft_ms: Number(row?.latency ?? 0), avg_cost_usd: Number(row?.cost ?? 0), run_count: Number(row?.count ?? 0) };
  }

  async listRuns(projectId: string): Promise<any[]> {
    const rows = await this.runs.find({
      where: { projectId },
      order: { timestamp: 'DESC' },
      take: 50,
    });
    return rows.map((r) => ({
      run_id: r.runId,
      dataset_id: r.datasetId,
      timestamp: r.timestamp.toISOString(),
      config_version: r.configVersion,
      telemetry: {
        avg_latency_ms: Number(r.avgLatencyMS),
        cost_usd: Number(r.costUSD),
        overall_score: Number(r.overallScore),
        passed: r.passed,
      },
      results: r.results,
      synced: true,
      source: 'postgres',
    }));
  }

  async listLocalRuns(): Promise<any[]> {
    const historyDir = path.resolve(process.cwd(), '../.caliper/history');
    if (!fs.existsSync(historyDir)) {
      // Also check root if CWD is api
      const altDir = path.resolve(__dirname, '../../../../.caliper/history');
      if (!fs.existsSync(altDir)) return [];
    }
    const runs: any[] = [];
    const walk = (dir: string) => {
      if (!fs.existsSync(dir)) return;
      const files = fs.readdirSync(dir);
      for (const file of files) {
        const fullPath = path.join(dir, file);
        const stat = fs.statSync(fullPath);
        if (stat.isDirectory()) {
          walk(fullPath);
        } else if (file.endsWith('.json') && !file.endsWith('.tmp')) {
          try {
            const content = fs.readFileSync(fullPath, 'utf8');
            const data = JSON.parse(content);
            if (data.run_id) {
              data.source = 'local';
              data.filePath = fullPath;
              runs.push(data);
            }
          } catch (e) {
            // ignore corrupt files
          }
        }
      }
    };
    walk(path.resolve(process.cwd(), '../.caliper/history'));
    if (runs.length === 0) {
      walk(path.resolve(process.cwd(), '.caliper/history'));
    }
    runs.sort((a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime());
    return runs;
  }
}
