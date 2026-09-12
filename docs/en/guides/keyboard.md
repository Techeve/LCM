---
sidebar:
  order: 30
title: Keyboard & accessibility
description: Keyboard shortcuts, mouse-free operation, screen readers and contrast in the LCM interface.
---

The interface can be operated entirely with the keyboard, and it is built
for screen readers: landmarks, labelled controls, headings in order, live
announcements on page changes and messages, contrast to WCAG 2.1 AA in both
colour modes.

## Keyboard shortcuts

**`?`** opens the overview of all shortcuts at any time - also via the `?`
button at the top right of the bar and the link in the footer. The overview
additionally shows the shortcuts of the page currently open.

Shortcuts apply while no input field has focus. Whoever types, types letters -
only Save (`Ctrl+S` or `⌘S`) also works inside a field.

### Jump

Two keys in a row: first `G`, then the letter of the page.

| Shortcut | Target |
|---|---|
| `G` `D` | Dashboard |
| `G` `G` | Groups |
| `G` `U` | Linux users |
| `G` `S` | Security |
| `G` `C` | Docker |
| `G` `J` | Jobs |
| `G` `E` | Settings |
| `G` `A` | My account |
| `G` `H` | Docs |

Pages the account has no permission for are missing from the overview and do
not respond.

### Everywhere

| Shortcut | Effect |
|---|---|
| `?` | Open or close the shortcut overview |
| `/` | Jump to the search field of the page |
| `Ctrl+S` / `⌘S` | Save the form |
| `Esc` | Close dialog or menu |
| `←` `→` | Move between controls in the main navigation and within table rows |
| `↑` `↓` | Move between entries in menus, between rows in tables |
| `Home` / `End` | First / last entry of a menu |
| `Tab` / `Shift+Tab` | Next / previous control |

### On the page

Buttons with a shortcut of their own carry it as a small badge right next to
the name - **"+ Add server `N`"**. The badge shows the key that triggers the
button; the overview (`?`) lists all shortcuts of the page under *On this
page*. The badges can be switched off there; the browser remembers the choice.

| Shortcut | Effect |
|---|---|
| `N` | Create new: server, group, Linux user, profile, rule, channel, application, repository, allowlist, custom action |
| `U` | Security: update all VMs |
| `/` | Search field: dashboard (name), jobs, packages of a server, rule blocks |

## Through the page without a mouse

- **Skip link.** The first `Tab` on every page lands on "Skip to content" -
  `Enter` skips the navigation bar.
- **Page changes.** After a change the focus is in the content, the window
  title names the page ("Jobs · LCM"), and a screen reader reads the page
  name.
- **Dialogs.** An opened dialog takes the focus (first field), `Tab` stays
  inside, `Esc` closes it, and the focus returns to the triggering button.
- **Messages.** Success messages are announced politely, errors interrupt -
  via two separate live regions.
- **Visible focus.** The keyboard focus is shown as a strong ring, also on the
  dark top bar. Mouse clicks show no ring.

## Contrast and motion

All text reaches 4.5:1 (WCAG AA) - in light and dark mode, also on the yellow
and red tinted table rows of the security overview. Anyone who has set
*reduce motion* in the operating system gets no fade-in and hover animations.

## Checked automatically

The test run checks the most important pages in both colour modes with
[axe-core](https://github.com/dequelabs/axe-core) against WCAG 2.1 AA
(`frontend/e2e/a11y.spec.js`) and keyboard operation with Playwright
(`frontend/e2e/hotkeys.spec.js`). A new violation shows up in the test run,
not first for the user.
