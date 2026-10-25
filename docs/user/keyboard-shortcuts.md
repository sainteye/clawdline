# Keyboard shortcuts

Open **Keyboard shortcuts** in the navigation menu, press `?`, or press **⌘/** on macOS
(**Ctrl+/** on Windows and Linux) to see the cheat sheet. It groups navigation, common actions,
Session controls and message editing, and shows the modifier for your keyboard. Close it with
Escape, its Close button, or a click outside; focus returns to the control you came from.

The shortcuts work in the local console and Clawdline Cloud. A terminal keeps its own keys;
press F6 to return keyboard focus to the page before using console shortcuts.

## Navigation

| macOS | Windows / Linux | Does |
| --- | --- | --- |
| ⌘K | Ctrl+K | Go to the Session list and focus it; the all-machines list focuses its search field |
| ⌘B | Ctrl+B | Go to Board |
| ⌘\ | Ctrl+\ | Open or close navigation |
| ⌘/ or `?` | Ctrl+/ or `?` | Open or close the cheat sheet |
| Esc | Esc | Close the top sheet or menu, clear the Session filter, step back to the Session list, or close the open detail |

## Common actions

These letters work outside text fields, menus and pickers that use letter keys. They open the same controls as the
buttons: they never send a message, create an item without review, or close a Session.

| Key | Does |
| --- | --- |
| `n` | Open New Session (or New Terminal when the terminal list is selected) |
| `t` | Open a new Board work item |
| `v` | Open voice command and begin listening through the existing microphone permission flow |
| `c` | Focus the open Session's message box |
| `a` | Open or close the open Session's attention notes |

Typing, IME composition and modal dialogs keep their own keys. **⌘B / Ctrl+B** also stands down
in text fields so editor formatting stays available. Holding a new shortcut does not repeat its
action. New Session and voice command first return to the Session page; they retain Cloud's
existing machine chooser when a machine must be selected. Message and attention shortcuts need
an open Session.

## In a Session

| Key | Does |
| --- | --- |
| ↑ / ↓ or `j` / `k` | Move through the local Session list |
| Return | Open the selected local Session |
| `/` | Filter the Session list |
| ⌘J / Ctrl+J | Show or hide the transcript; on a phone, switch between list and detail |
| ⌘I / Ctrl+I | Open or close Session info |
| `g` / `G` | Top or bottom of the transcript |
| `r` | Reverse the transcript's order |

## In the message box

| Key | Does |
| --- | --- |
| Return | Send (on a touch screen, a new line; send with the button) |
| Shift-Return | New line |
| `/` as the whole message (`$` for Codex) | List the Session's skills and commands. ↑ ↓ move, Tab or Return choose, Esc closes. Choosing one never sends |

## In menus and sheets

- On the **⋯** button, ↓ opens the menu; Home, End, ↑ and ↓ move; ← or Esc leaves the Git submenu.
- Esc closes the snippets, start, to-do and picture-marking sheets and confirmation dialogs.
- The cheat sheet keeps Tab and Shift-Tab inside its dialog. Navigation shortcuts do not move
  the page underneath an open dialog.
