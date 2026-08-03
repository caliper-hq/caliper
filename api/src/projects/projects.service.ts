import { Injectable, OnModuleInit } from '@nestjs/common';
import { InjectRepository } from '@nestjs/typeorm';
import { Repository } from 'typeorm';
import { Project } from './project.entity';
@Injectable()
export class ProjectsService implements OnModuleInit {
  constructor(@InjectRepository(Project) private readonly projects: Repository<Project>) {}
  async onModuleInit(): Promise<void> {
    const id = process.env.BOOTSTRAP_PROJECT_ID;
    const apiKey = process.env.BOOTSTRAP_PROJECT_API_KEY;
    if (id && apiKey && !(await this.projects.existsBy({ id }))) await this.projects.save({ id, name: id, apiKey });
  }
  async hasKey(id: string, key: string): Promise<boolean> {
    const project = await this.projects.findOneBy({ id });
    return !!project && project.apiKey === key;
  }
}
