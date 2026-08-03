import { Body, Controller, Get, Post, Query, Headers } from '@nestjs/common';
import { PullRequestDto } from './git-bridge.dto';
import { GitBridgeService } from './git-bridge.service';

@Controller('git-bridge')
export class GitBridgeController {
  constructor(private readonly bridge: GitBridgeService) {}

  @Get('files')
  listFiles() {
    return this.bridge.listFiles();
  }

  @Get('file-content')
  getFileContent(@Query('path') path: string) {
    return this.bridge.getFileContent(path);
  }

  @Post('pull-request')
  create(@Body() input: PullRequestDto, @Headers('x-user-github-token') userToken?: string) {
    return this.bridge.createPullRequest(input, userToken);
  }
}
