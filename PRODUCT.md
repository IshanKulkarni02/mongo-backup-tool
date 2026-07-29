# Product

## Register

product

## Users

MongoDB developers, operators, and small teams working locally or with Atlas who need safe, frequent checkpoints, inspectable history, portable backups, and reliable recovery without maintaining separate tools for each workflow. They use mongobak from scripts, an interactive terminal, or a desktop manager while developing, migrating, debugging, and operating databases.

## Product Purpose

mongobak is a cross-platform “Git for MongoDB” manager. It combines full-fidelity database-tool archives with content-addressed, deduplicated, diffable snapshots and exposes the same core behavior through CLI, TUI, and desktop interfaces. Success means users can understand what changed, create and restore checkpoints safely, browse and manage data at scale, automate retention and schedules, and move snapshot history through Git/LFS without hidden credential handling or unbounded resource use.

## Brand Personality

Precise, trustworthy, and developer-native. The product should feel calm under operational pressure, explicit about destructive consequences, and dense without becoming cryptic.

## Anti-references

Avoid marketing-dashboard decoration, glassmorphism, gradients, oversized metrics, excessive rounding, ornamental shadows, emoji-led controls, and low-density card grids. Avoid interfaces that hide database scope, destructive impact, dependency state, consistency guarantees, or job progress behind vague copy. Do not imitate a consumer backup app or an abstract SaaS analytics dashboard.

## Design Principles

1. Make scope and consequence unmistakable before every database-changing action.
2. Keep CLI, TUI, and desktop behavior aligned by sharing core services and terminology.
3. Prefer bounded, inspectable operations that remain safe on large databases.
4. Reveal technical truth—consistency mode, dependency state, storage backend, progress, and errors—without forcing users to guess.
5. Optimize expert workflows while keeping first-run recovery and guidance approachable.

## Accessibility & Inclusion

Target WCAG 2.2 AA for the desktop interface. Support full keyboard operation, visible focus, semantic labels and states, non-color-only status communication, reduced motion, text scaling, and high-contrast light and dark themes. Terminal workflows must remain understandable without relying on color alone.
