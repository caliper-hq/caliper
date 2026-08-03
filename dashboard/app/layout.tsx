import './globals.css';
import type { Metadata } from 'next';
export const metadata: Metadata = { title: 'Caliper Dashboard', description: 'Git-backed AI evaluation suites' };
export default function Layout({ children }: Readonly<{ children: React.ReactNode }>) { return <html lang="en"><body>{children}</body></html>; }
