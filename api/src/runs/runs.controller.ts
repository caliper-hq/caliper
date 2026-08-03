import { Body, Controller, Get, Param, Post, UseGuards } from '@nestjs/common';
import { ProjectApiKeyGuard } from '../projects/project-api-key.guard';
import { BulkRunsDto } from './run.dto';
import { RunsService } from './runs.service';

@Controller('projects/:id')
export class RunsController {
  constructor(private readonly runs: RunsService) {}

  @Post('runs')
  @UseGuards(ProjectApiKeyGuard)
  ingest(@Param('id') id: string, @Body() body: BulkRunsDto) {
    return this.runs.ingest(id, body.runs);
  }

  @Get('metrics')
  metrics(@Param('id') id: string) {
    return this.runs.metrics(id);
  }

  @Get('runs/list')
  listRuns(@Param('id') id: string) {
    return this.runs.listRuns(id);
  }

  @Get('runs/local')
  listLocalRuns() {
    return this.runs.listLocalRuns();
  }
}
