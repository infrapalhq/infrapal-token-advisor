# How accurate the numbers are

Claude Code's telemetry is detailed, but it doesn't record everything. This page
says which figures you can rely on, and where token-advisor is making a judgment.

## Confidence labels

Every issue and fix in the dashboard carries a label:

| Label | Means | Example |
| --- | --- | --- |
| **Measured** | Read directly from telemetry | Cost, token counts, approvals, compactions |
| **Likely** | A rule applied to measured data, which can be wrong in some cases | A file re-read that wasn't needed |
| **Estimated** | A derived figure, shown with `≈` | Tokens for a tool result, the dollar value of waste |

No figure comes from an AI model. AI Mode only adds recommendations, and it
checks them against your real figures, dropping any that cite something else.

## What's exact

- **Cost:** Claude Code reports it per API request. It's what Claude Code
  calculates, not your invoice, so it may not match a plan or subscription bill.
- **Tokens:** input, output, cache read and cache creation, per request.
- **Models, requests, retries and API errors.**
- **Tool calls:** which tool ran, whether it was approved, and whether by you or
  by a rule.
- **Compactions:** tokens before and after each one.

## What's estimated or inferred

| Figure | How it's worked out | What can be off |
| --- | --- | --- |
| Tool result tokens | Result size in bytes ÷ 4, as telemetry doesn't report tokens per tool | Rough, particularly for code or non-English text |
| Dollar value of waste | Tokens × your own average cost per token | Understated: a large result is often re-sent as cached input on later requests |
| Context per step | Each tool row takes the context of the next API request | Approximate per step; peak context and compactions are exact |
| Project | The session transcript's working directory, else the nearest `.git` above the files touched, else their common folder | Claude Code deletes transcripts after 30 days, so it's read while it exists. A session with neither has no project |
| Waiting on you | Time blocked on your approval, capped at 10 minutes per approval | Longer waits count as idle, as you probably walked away |
| Estimated saving | Baseline at the time you mark a fix, compared with afterwards, for at least 7 days | Your work may have changed too, so it isn't proof of cause |

## What each issue means

| Issue | Rule | Label | Can be wrong when |
| --- | --- | --- | --- |
| Re-read file | The same lines are read again in one prompt with nothing changing the file | Likely | You or another tool changed the file outside Claude |
| Repeated command | The same command runs again in one prompt with no file change | Likely | The same, or the command's output depends on something else, such as a deploy |
| Repeated approval | You approved the same pattern by hand two or more times | Measured | |
| Failing MCP server | At least 5 calls, with 20% or more failing | Measured | |
| Compaction | Claude Code compacted the session's context | Measured | |
| Oversized model | An Opus-class model gave a short answer, up to 2,000 tokens, with no file changes and few tools | Likely | A short answer can still need the larger model's judgment |

token-advisor only sees what Claude does, so a change you make in your editor is
invisible to it. That's why rules that depend on "nothing changed" are Likely.

An allow rule is only suggested for read-only commands on a fixed list. A pattern
outside that list is reported with no fix to paste, so a suggestion never widens
what Claude can write or change.

## What depends on your settings

- **Prompt and response text:** only present while `init`'s content logging
  settings are on.
- **Session titles:** come from Claude Code's own titles, which need responses to
  be logged.
- **Newer attributes:** some details only appear with newer Claude Code versions.

Telemetry only exists from when you ran `init`. Sessions before that, or while the
collector was down, aren't captured, because Claude Code doesn't buffer it. Trends
that compare with your first weeks need some history before they appear.
