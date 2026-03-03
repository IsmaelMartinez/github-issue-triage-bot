# Batch 0: Baseline Validation

Date: 2026-03-03
Bot deployment: Cloud Run image v9 (pre-security-hardening)
Database: 111 issues, 18 documents (14 troubleshooting + 4 configuration), 0 feature index entries

## Issue #10: Bug with missing template sections (Phase 1 test)

Title: `[Baseline] App crashes on startup after update`
Labels: bug
Body: minimal, no template headers

**Bot response:** Phases 2 and 3 fired. Phase 1 (missing info) did NOT produce a missing-items checklist despite the body having no template sections. This may be because the issue lacks the "bug" template structure entirely (Phase 1 looks for specific headers and only flags missing ones when at least some are present).

Phases that fired: Phase 2 (solution suggestion: "Application fails to launch after update"), Phase 3 (duplicate: #2132 segfault)

**Notable:** No missing-info checklist was shown even though the issue body is a single sentence with no debug info, reproduction steps, or expected behavior.


## Issue #11: Bug matching troubleshooting docs (Phase 2 test)

Title: `[Baseline] Screen sharing shows black window on Wayland`
Labels: bug
Body: complete template with all sections filled

**Bot response:** Phase 2 matched "Blank or black window on Wayland" troubleshooting doc with actionable suggestion (ELECTRON_OZONE_PLATFORM_HINT=x11). Phase 3 found #1923 (screen sharing on Wayland) and #1184 (screen sharing not working).

Phases that fired: Phase 2 (1 match), Phase 3 (2 duplicates, both closed)

**Notable:** Excellent match quality on Phase 2 and Phase 3.


## Issue #12: Duplicate bug (Phase 3 test)

Title: `[Baseline] Notifications not working on Linux`
Labels: bug
Body: template with all sections

**Bot response:** Phase 2 matched "No Desktop Notifications" troubleshooting doc. Phase 3 found #1234 (meeting notifications) and #1908 (notifications disappear on KDE Plasma 5).

Phases that fired: Phase 2 (1 match), Phase 3 (2 duplicates, both closed)

**Notable:** Good matches, relevant duplicates found from the 111-issue dataset.


## Issue #13: Enhancement request (Phase 4a test)

Title: `[Baseline] Add support for custom themes`
Labels: enhancement
Body: short feature request

**Bot response:** No comment posted.

Phases that fired: None

**Notable:** Expected — no feature index data is seeded, so Phase 4a has nothing to match. Phase 4b did not fire because "enhancement" is the correct label. The comment builder returned empty string since no phases produced output.


## Issue #14: Misclassified question (Phase 4b test)

Title: `[Baseline] How do I change the notification sound?`
Labels: bug (intentionally wrong)
Body: question about settings

**Bot response:** Phase 2 matched notifications troubleshooting doc (loose match). Phase 3 found #1977 (Custom Notification System MVP). Phase 4b correctly detected misclassification: suggested relabelling as `question` with clear reasoning.

Phases that fired: Phase 2 (1 match), Phase 3 (1 duplicate), Phase 4b (label suggestion: question)

**Notable:** Phase 4b works correctly. The Phase 2/3 matches are tangentially relevant but not ideal — they're about notifications in general, not about notification sounds specifically.


## Summary

| Issue | Phase 1 | Phase 2 | Phase 3 | Phase 4a | Phase 4b | Comment posted |
|-------|---------|---------|---------|----------|----------|----------------|
| #10   | -       | 1 match | 1 dup   | -        | -        | Yes            |
| #11   | -       | 1 match | 2 dups  | -        | -        | Yes            |
| #12   | -       | 1 match | 2 dups  | -        | -        | Yes            |
| #13   | -       | -       | -       | 0 match  | -        | No             |
| #14   | -       | 1 match | 1 dup   | -        | question | Yes            |

Observations for follow-up:
- Phase 1 did not fire on #10 despite completely missing template. Investigate phase1.go logic.
- Phase 4a needs feature index data to be useful (addressed in Batch 5).
- Phase 2 and 3 produce good matches even with only 111 issues and 18 docs.
