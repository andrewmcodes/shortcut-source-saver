# Nested If

A backup of the **Nested If** shortcut. `shortcut.json` is the canonical, diffable source; `shortcut.shortcut` is the signed binary that the Shortcuts app can import.

| | |
| --- | --- |
| Actions | 8 |
| Created | 2025-03-01 09:30 UTC |
| Modified | 2025-06-14 17:05 UTC |
| Icon | `#4F9BFF` (glyph 59511) |
| Minimum iOS | 18 |
| Client version | 3216 |
| Minimum client version | 3010 |
| Workflow types | ActionExtension, NCWidget, WFWorkflowTypeShowInSearch |

[Import the shortcut](shortcut.shortcut) · [Open the web review](review.html)

## Actions

- `0` If [Answer] is yes
  - `1` Text
  - `2` If All are true; [1 Text] contains the; [Answer] has any value
    - `3` Show Content
  - `4` End If
- `5` Otherwise if [Count] is between 1 and 9
  - `6` Nothing
- `7` End If
