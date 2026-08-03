# `caliper-dashboard`

[![GitHub License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Next.js](https://img.shields.io/badge/Framework-Next.js%2015-black.svg)](https://nextjs.org)
[![Tailwind CSS](https://img.shields.io/badge/Styling-Tailwind%20CSS-38bdf8.svg)](https://tailwindcss.com)
[![Org](https://img.shields.io/badge/Org-caliper--hq-orange.svg)](https://github.com/caliper-hq)

The visual suite editor and prompt playground for **[Caliper](https://github.com/caliper-hq/caliper)**. `caliper-dashboard` allows prompt engineers, product teams, and developers to visually craft evaluation datasets, prompt templates, and Regex or Semantic assertion rules—without writing manual YAML.

---

## 🌟 Key Features

- **🎨 Visual Suite Editor**: Interactive UI to build and tune prompt datasets, system instructions, and assertion thresholds.
- **🔒 Zero-Lock-In Database Design**: Deliberately stores no suite database. Clicking **Save suite** serializes `caliper.yml` and sends it to [`caliper-api`](https://github.com/caliper-hq/caliper-api), which automatically opens a Git Pull Request.
- **🛡️ Secure Client State**: Project API keys are held transiently in browser memory to authorize Git PR actions—never persisted on disk or sent to unverified remote servers.
- **📱 Responsive & Fast**: Built with Next.js 15 App Router, React Server Components, and Tailwind CSS.

---

## 🛠️ Quickstart

### Local Development

1. Install dependencies:
   ```bash
   npm install
   ```

2. Start development server:
   ```bash
   npm run dev
   ```

3. Open `http://localhost:3001` (or `http://localhost:3000`).

### Environment Variables

Configure the API endpoint connection by creating a `.env.local` file:

```env
NEXT_PUBLIC_API_URL=http://localhost:3000
```

---

## 🔗 Related Repositories

- **[`caliper-hq/caliper`](https://github.com/caliper-hq/caliper)**: High-performance Go CLI benchmark runner.
- **[`caliper-hq/caliper-api`](https://github.com/caliper-hq/caliper-api)**: Central control plane & Git integration service.
