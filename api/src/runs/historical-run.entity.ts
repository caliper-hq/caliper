import { Column, CreateDateColumn, Entity, Index, JoinColumn, ManyToOne, PrimaryGeneratedColumn } from 'typeorm';
import { Project } from '../projects/project.entity';
@Entity('historical_runs')
@Index(['projectId', 'runId'], { unique: true })
@Index(['projectId', 'timestamp'])
export class HistoricalRun {
  @PrimaryGeneratedColumn() id!: number;
  @Column({ name: 'project_id' }) projectId!: string;
  @ManyToOne(() => Project, (project) => project.runs, { onDelete: 'CASCADE' }) @JoinColumn({ name: 'project_id' }) project!: Project;
  @Column({ name: 'run_id' }) runId!: string;
  @Column({ name: 'dataset_id' }) datasetId!: string;
  @Column({ name: 'config_version' }) configVersion!: string;
  @Column({ type: 'timestamptz' }) timestamp!: Date;
  @Column({ type: 'bigint', name: 'avg_latency_ms' }) avgLatencyMS!: string;
  @Column({ type: 'float', name: 'cost_usd' }) costUSD!: number;
  @Column({ type: 'float', name: 'overall_score' }) overallScore!: number;
  @Column({ type: 'boolean' }) passed!: boolean;
  @Column({ type: 'jsonb', default: () => "'[]'" }) results!: unknown[];
  @CreateDateColumn({ name: 'received_at' }) receivedAt!: Date;
}
