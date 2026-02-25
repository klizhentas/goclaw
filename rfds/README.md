# RFD Index

This folder is the canonical design source for goclaw.

## Ordering and Precedence
1. `RFD 0000` defines the baseline core architecture.
2. Higher-numbered accepted RFDs extend or refine scoped behavior.
3. If scope overlaps, the newer accepted RFD wins for that scope.
4. Legacy docs outside `rfds/` are informational unless explicitly marked canonical.

## RFDs
- `0000-core-v1.md` - Core system design (v1 baseline)
- `0001-task-scheduler.md` - Scheduled task execution (SQLite + queue)
- `0002-repo-guidelines.md` - Repository standards and development guidelines
