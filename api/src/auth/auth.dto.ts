import { IsOptional, IsString, MinLength } from 'class-validator';

export class RegisterDto {
  @IsString()
  @MinLength(1)
  username!: string;

  @IsString()
  @MinLength(1)
  password!: string;

  @IsString()
  @IsOptional()
  githubToken?: string;

  @IsString()
  @IsOptional()
  repository?: string;

  @IsString()
  @IsOptional()
  projectId?: string;
}

export class LoginDto {
  @IsString()
  @MinLength(1)
  username!: string;

  @IsString()
  @MinLength(1)
  password!: string;

  @IsString()
  @IsOptional()
  githubToken?: string;

  @IsString()
  @IsOptional()
  repository?: string;

  @IsString()
  @IsOptional()
  projectId?: string;
}

export class UpdateKeysDto {
  @IsString()
  @IsOptional()
  githubToken?: string;

  @IsString()
  @IsOptional()
  repository?: string;

  @IsString()
  @IsOptional()
  projectId?: string;
}

export class ForgotPasswordDto {
  @IsString()
  @MinLength(1)
  username!: string;
}

export class ResetPasswordDto {
  @IsString()
  @MinLength(1)
  username!: string;

  @IsString()
  @MinLength(1)
  resetToken!: string;

  @IsString()
  @MinLength(1)
  newPassword!: string;
}
