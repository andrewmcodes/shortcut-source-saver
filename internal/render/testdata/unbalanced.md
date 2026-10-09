# Unbalanced Blocks

A backup of the **Unbalanced Blocks** shortcut. `shortcut.json` is the canonical, diffable source; `shortcut.shortcut` is the signed binary that the Shortcuts app can import.

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
> The control flow in this shortcut does not balance: action 0 opens or closes a block that has no counterpart. The web review flags it.

## Actions

- `0` End If — **corrupted block**
- `1` Text
- `2` If [Thing] does not have any value
  - `3` Nothing
- `4` End Repeat Times — **corrupted block**
- `5` If [Thing] has any value
  - `6` Text
