import { Type } from 'class-transformer';
import {
  IsArray,
  IsBoolean,
  IsDateString,
  IsInt,
  IsNumber,
  IsOptional,
  IsString,
  ValidateNested,
} from 'class-validator';

/**
 * TelemetryDto mirrors the Go storage.Telemetry struct exactly.
 * All fields beyond the four core aggregates are optional so that older CLI
 * versions (pre-Phase 2) can still sync without validation errors.
 */
export class TelemetryDto {
  // Core aggregates — always present
  @IsNumber() avg_latency_ms!: number;
  @IsNumber() cost_usd!: number;
  @IsNumber() overall_score!: number;
  @IsBoolean() passed!: boolean;

  // Extended telemetry added in Phase 2 — optional for backward compatibility
  @IsOptional() @IsInt() total_latency_ms?: number;
  @IsOptional() @IsInt() eval_count?: number;
  @IsOptional() @IsInt() passed_count?: number;
  @IsOptional() @IsInt() failed_count?: number;
  @IsOptional() @IsInt() skipped_count?: number;
}

/**
 * RunDto mirrors the Go storage.RunRecord struct.
 * `synced` is a local-only flag; we accept it from the CLI but do not store it
 * (the control plane is the source of truth for sync status).
 */
export class RunDto {
  @IsString() run_id!: string;
  @IsString() dataset_id!: string;
  @IsString() config_version!: string;
  @IsDateString() timestamp!: string;

  @ValidateNested()
  @Type(() => TelemetryDto)
  telemetry!: TelemetryDto;

  @IsArray() @IsOptional() results?: unknown[];

  /** Accepted but ignored — the control plane tracks its own sync state. */
  @IsBoolean() @IsOptional() synced?: boolean;
}

export class BulkRunsDto {
  @IsArray()
  @ValidateNested({ each: true })
  @Type(() => RunDto)
  runs!: RunDto[];
}
