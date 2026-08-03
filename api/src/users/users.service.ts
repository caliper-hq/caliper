import { Injectable, ConflictException, UnauthorizedException, NotFoundException } from '@nestjs/common';
import { InjectRepository } from '@nestjs/typeorm';
import { Repository } from 'typeorm';
import { User } from './user.entity';
import * as crypto from 'crypto';

@Injectable()
export class UsersService {
  constructor(
    @InjectRepository(User)
    private readonly userRepository: Repository<User>,
  ) {}

  private hashPassword(password: string): string {
    return crypto.createHash('sha256').update(password).digest('hex');
  }

  async register(username: string, password: string, githubToken?: string, repository?: string, projectId?: string) {
    const existing = await this.userRepository.findOneBy({ username });
    if (existing) {
      throw new ConflictException('Username already taken');
    }
    const user = this.userRepository.create({
      username,
      passwordHash: this.hashPassword(password),
      githubToken: githubToken || '',
      repository: repository || 'owner/repository',
      projectId: projectId || 'default',
    });
    await this.userRepository.save(user);
    const { passwordHash, ...result } = user;
    return result;
  }

  async login(username: string, password: string) {
    const user = await this.userRepository.findOneBy({ username });
    if (!user || user.passwordHash !== this.hashPassword(password)) {
      throw new UnauthorizedException('Invalid credentials');
    }
    const { passwordHash, ...result } = user;
    return result;
  }

  async findById(id: string) {
    const user = await this.userRepository.findOneBy({ id });
    if (!user) throw new NotFoundException('User not found');
    const { passwordHash, ...result } = user;
    return result;
  }

  async updateKeys(id: string, githubToken?: string, repository?: string, projectId?: string) {
    const user = await this.userRepository.findOneBy({ id });
    if (!user) throw new NotFoundException('User not found');
    if (githubToken !== undefined) user.githubToken = githubToken;
    if (repository !== undefined) user.repository = repository;
    if (projectId !== undefined) user.projectId = projectId;
    await this.userRepository.save(user);
    const { passwordHash, ...result } = user;
    return result;
  }

  async requestPasswordReset(username: string) {
    const user = await this.userRepository.findOneBy({ username });
    if (!user) {
      throw new NotFoundException('User with specified username not found');
    }
    const resetToken = crypto.randomBytes(16).toString('hex');
    const expires = new Date();
    expires.setHours(expires.getHours() + 1); // Token valid for 1 hour

    user.resetToken = resetToken;
    user.resetTokenExpires = expires;
    await this.userRepository.save(user);

    return {
      message: 'Password reset token generated successfully',
      resetToken,
      username,
    };
  }

  async resetPassword(username: string, resetToken: string, newPassword: string) {
    const user = await this.userRepository.findOneBy({ username });
    if (!user || !user.resetToken || user.resetToken !== resetToken) {
      throw new UnauthorizedException('Invalid or expired reset token');
    }
    if (user.resetTokenExpires && user.resetTokenExpires < new Date()) {
      throw new UnauthorizedException('Reset token has expired');
    }

    user.passwordHash = this.hashPassword(newPassword);
    user.resetToken = undefined;
    user.resetTokenExpires = undefined;
    await this.userRepository.save(user);

    return { message: 'Password has been reset successfully' };
  }
}
