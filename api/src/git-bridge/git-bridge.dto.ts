import { IsString, Matches } from 'class-validator';
export class PullRequestDto {
  @IsString() project_id!: string;
  @IsString() @Matches(/^[\w.-]+\/[\w.-]+$/) repository!: string;
  @IsString() yaml!: string;
  @IsString() @Matches(/^[\w./-]+\.ya?ml$/) file_path!: string;
  @IsString() title!: string;
  @IsString() @Matches(/^[\w./-]+$/) branch_name!: string;
}
