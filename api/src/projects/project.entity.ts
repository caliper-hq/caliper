import { Column, Entity, OneToMany, PrimaryColumn } from 'typeorm';
import { HistoricalRun } from '../runs/historical-run.entity';
@Entity('projects')
export class Project {
  @PrimaryColumn('varchar') id!: string;
  @Column({ default: '' }) name!: string;
  @Column({ name: 'api_key' }) apiKey!: string;
  @OneToMany(() => HistoricalRun, (run) => run.project) runs!: HistoricalRun[];
}
