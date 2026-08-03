import { CanActivate, ExecutionContext, Injectable, UnauthorizedException } from '@nestjs/common';
import { ProjectsService } from './projects.service';
@Injectable()
export class ProjectApiKeyGuard implements CanActivate {
  constructor(private readonly projects: ProjectsService) {}
  async canActivate(context: ExecutionContext): Promise<boolean> {
    const request = context.switchToHttp().getRequest<{ header(name: string): string | undefined; params: Record<string, unknown>; body?: Record<string, unknown> }>();
    const value = request.header('authorization') ?? '';
    const match = /^Bearer\s+(.+)$/i.exec(value);
    const projectId = String(request.params.id ?? request.body?.project_id ?? '');
    if (!match || !(await this.projects.hasKey(projectId, match[1]))) throw new UnauthorizedException('valid project API key required');
    return true;
  }
}
