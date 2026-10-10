# Malformed Values

A backup of the **Malformed Values** shortcut. `shortcut.json` is the canonical, diffable source; `shortcut.shortcut` is the signed binary that the Shortcuts app can import.

| | |
| --- | --- |
| Actions | 7 |
| Created | 2025-03-01 09:30 UTC |
| Modified | 2025-06-14 17:05 UTC |
| Icon | `#4F9BFF` (glyph 59511) |
| Minimum iOS | 18 |
| Client version | 3216 |
| Minimum client version | 3010 |
| Workflow types | ActionExtension, NCWidget, WFWorkflowTypeShowInSearch |
| Accepts | Text |

[Import the shortcut](shortcut.shortcut) · [Open the web review](review.html)

> [!WARNING]
> Action 1 stores a value with the wrong type: WFCondition is the string "Equals", not an operator code; WFControlFlowMode is the string "0", not an integer. The web review flags it.

## Actions

- `0` Text
- `1` If [0 Text] Equals expected value — **malformed value**
  - `2` Nothing
- `3` End If — **malformed value**
- `4` If [0 Text] operator 42 operand
- `5` End If
- `6` If — **malformed value**
