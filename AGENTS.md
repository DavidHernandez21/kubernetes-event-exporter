# Repository Notes

## Go Test Timing

- For concurrent or timing-sensitive Go tests, prefer `testing/synctest` over real-time waits.
- Do not add new tests that rely on `time.After`, long `time.Sleep`, or wall-clock timing to prove ordering or eventual consistency.
- Use `synctest.Test` to run the test in a bubble, `synctest.Wait` to let goroutines settle before assertions, and `synctest.Sleep` when advancing fake time is part of the behavior under test.
- If code under test depends on timers, contexts with deadlines, or goroutine coordination, write the test so it is deterministic under `synctest` rather than waiting on the real clock.
- If `synctest` is not a fit because the test depends on external I/O or goroutines outside the bubble, prefer a fake clock, fake network, or other deterministic test seam rather than real-time delays.
