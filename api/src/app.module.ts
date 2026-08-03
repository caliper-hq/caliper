import { Module } from '@nestjs/common';
import { TypeOrmModule } from '@nestjs/typeorm';
import { Project } from './projects/project.entity';
import { HistoricalRun } from './runs/historical-run.entity';
import { User } from './users/user.entity';
import { ProjectsService } from './projects/projects.service';
import { ProjectApiKeyGuard } from './projects/project-api-key.guard';
import { RunsController } from './runs/runs.controller';
import { RunsService } from './runs/runs.service';
import { GitBridgeController } from './git-bridge/git-bridge.controller';
import { GitBridgeService } from './git-bridge/git-bridge.service';
import { AuthController } from './auth/auth.controller';
import { UsersService } from './users/users.service';

@Module({
  imports: [
    TypeOrmModule.forRoot({
      type: 'postgres',
      url: process.env.DATABASE_URL,
      entities: [Project, HistoricalRun, User],
      synchronize: true,
    }),
    TypeOrmModule.forFeature([Project, HistoricalRun, User]),
  ],
  controllers: [RunsController, GitBridgeController, AuthController],
  providers: [ProjectsService, ProjectApiKeyGuard, RunsService, GitBridgeService, UsersService],
})
export class AppModule {}
