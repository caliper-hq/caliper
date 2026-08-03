import { Column, CreateDateColumn, Entity, PrimaryGeneratedColumn } from 'typeorm';

@Entity('users')
export class User {
  @PrimaryGeneratedColumn('uuid')
  id!: string;

  @Column({ unique: true })
  username!: string;

  @Column()
  passwordHash!: string;

  @Column({ nullable: true })
  githubToken?: string;

  @Column({ nullable: true })
  repository?: string;

  @Column({ default: 'default' })
  projectId!: string;

  @Column({ nullable: true })
  resetToken?: string;

  @Column({ nullable: true, type: 'timestamp' })
  resetTokenExpires?: Date;

  @CreateDateColumn()
  createdAt!: Date;
}
