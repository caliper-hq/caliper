import { Body, Controller, Get, Param, Post, Put } from '@nestjs/common';
import { UsersService } from '../users/users.service';
import { RegisterDto, LoginDto, UpdateKeysDto, ForgotPasswordDto, ResetPasswordDto } from './auth.dto';

@Controller('auth')
export class AuthController {
  constructor(private readonly usersService: UsersService) {}

  @Post('register')
  register(@Body() body: RegisterDto) {
    return this.usersService.register(body.username, body.password, body.githubToken, body.repository, body.projectId);
  }

  @Post('login')
  login(@Body() body: LoginDto) {
    return this.usersService.login(body.username, body.password);
  }

  @Get('user/:id')
  getUser(@Param('id') id: string) {
    return this.usersService.findById(id);
  }

  @Put('keys/:id')
  updateKeys(@Param('id') id: string, @Body() body: UpdateKeysDto) {
    return this.usersService.updateKeys(id, body.githubToken, body.repository, body.projectId);
  }

  @Post('forgot-password')
  forgotPassword(@Body() body: ForgotPasswordDto) {
    return this.usersService.requestPasswordReset(body.username);
  }

  @Post('reset-password')
  resetPassword(@Body() body: ResetPasswordDto) {
    return this.usersService.resetPassword(body.username, body.resetToken, body.newPassword);
  }
}
