# Exam Monitor — Polish Requirements (v0.2.x series)

**Status:** Draft (2026-05-18) — pending final user review.
**Author:** captured from the v0.1.0 → v0.1.8 iterative session, then re-framed as a proper requirements document.
**Scope:** the v0.2.x release series. After v0.2.2 the system should feel "polished and trustworthy" by classroom-instructor standards. v0.3.x is reserved for wire-protocol-breaking changes (deferred).

---

## 1. Purpose & scope

This document defines what a *polished* version of the exam-monitor app must do and how we know it's done. It is informed by:

- **The current implementation** (v0.1.0 → v0.1.8 ship history, hands-on testing feedback).
- **Upstream `khayrultw/exam-monitor`** — confirmed to be behind the current fork; not a useful reference.
- **Commercial proctoring software** (Respondus, Proctorio, Honorlock, ExamSoft / Examplify, ProctorU, Pearson VUE) — analyzed for *baseline* (not premium) feature patterns.
- **Open-source classroom tools** (Veyon, SafeExamBrowser conceptually) — for architecture and UX patterns.

In scope:
- The desktop server (instructor) and client (student) apps.
- Their UI, connection lifecycle, screen-capture pipeline, event log.
- Cross-platform parity (macOS arm64 + amd64, Linux amd64, Windows amd64).

Out of scope: see §7.

---

## 2. Glossary

| Term | Meaning |
|---|---|
| **Instructor** | The person running the server. Sees student screens. Starts/stops exam. |
| **Student** | The person running the client. Has their screen streamed to the instructor. |
| **Server** | The instructor's desktop app (`ExamMonitorTeacher`). Authoritative. |
| **Client** | The student's desktop app (`ExamMonitorStudent`). Driven by the server. |
| **Exam** | A scoped session. Has a name, room, token, start time, end time. Persisted as one directory under `<UserConfigDir>/exam-monitor/exams/`. |
| **Room** | A logical channel name (Alpha/Bravo/.../ + custom). Maps deterministically to a network port via `RoomNameToPort`. |
| **Token** | A short alphanumeric string (`XXX-XXX-NNN`) that students must present to join. Per-exam. |
| **Session state** | One of `Waiting`, `Capturing`, `Locked`, `Stopped`. Tracked by a state machine on both ends. |
| **Frame** | A single JPEG-encoded screenshot sent client→server. |
| **Control frame** | A non-image wire message (ExamStart, ExamStop, Lock, Unlock, Message, StateAck, TokenHandshake, etc.). |

---

## 3. Personas & key user journeys

### 3.1 Personas

- **Instructor (Sara, classroom teacher)** — sets up the exam, distributes the token verbally or via screen, monitors ~25 students simultaneously, sends "5 minutes remaining" announcements, exports CSV for records. Mid-skill technically; runs Mac. Doesn't tolerate flakiness.
- **Student (Yee Ling, exam-taker)** — boots client, types the token Sara just announced, sees the "monitored" banner, takes exam, doesn't think about the client app again until exam ends. Runs Windows. Low patience for app issues.

### 3.2 Key user journeys

**J1 — Exam setup (instructor)**
1. Open Server app. Pick room (e.g., Alpha). Type exam name. Copy token (clipboard).
2. Click **Open Waiting Room** → dashboard appears (0 students).
3. Tell students the token verbally / via projector / via paper.

**J2 — Student joins (student)**
1. Open Client app. Pick same room. Type student ID + name + token. Leave Server IP blank.
2. Click **Join**. mDNS finds the server. Client transitions to *Waiting*. Banner: "Waiting for your instructor…"
3. Student tile appears on instructor dashboard with green dot.

**J3 — Mid-exam disruption (student)**
1. Network blips for 8 s. Client's TCP connection drops.
2. Client banner switches to "Reconnecting… (3s, 4s, 5s)" with auto-retry every second.
3. Network returns; client re-handshakes (same token, no re-typing); banner restores to "Exam in progress". Frames resume.
4. Server log shows `student_disconnected … student_reconnected` events for the disputed time window.

**J4 — Instructor announcement (instructor)**
1. Instructor clicks **Send Message**, types "5 minutes left", clicks Send.
2. Within 1 s, every connected client shows a toast with the message for 5 s.
3. Server eventlog records `message_broadcast{delivered_to=[<list>]}`.

**J5 — Exam end (instructor)**
1. Instructor clicks **Stop Exam**. Confirmation dialog: "End exam for N students?".
2. Confirm. Each client transitions to "Exam ended". Capture loop stops.
3. Instructor clicks **Export Event Log**. CSV downloads to known path.

---

## 4. Functional Requirements

### v0.2.0 — "Visible polish" (highest pain-to-effort ratio)

#### FR-1 — Visible app version everywhere

The running version (`v0.2.0`, etc.) is shown at both the OS window-title level and inside the app's home/dashboard view.

**Why:** Instructors can't tell whether students are on a current version. The `Version` constant exists but is invisible. Commercial tools all show this for support-triage reasons.

**Done when:**
- Server window title reads `Exam Monitor v0.2.0` (or `Exam Monitor v0.2.0-local` for local builds). Title updates if the user enters a different room.
- Client window title reads `Exam Guard Client v0.2.0` similarly.
- Both apps render the version string in the bottom-right corner at 10 pt slate-400. Shown on: server home view, server dashboard view, client join view, client dashboard view. Not shown inside modals/dialogs.
- The README's "Verification" section shows a screenshot of the version visible.

---

#### FR-2 — Connection-state indicator with proper reconnect UX

The server dashboard shows three connection states per tile (green/yellow/red). The client handles disconnects gracefully without requiring an app relaunch.

**Why:** Today, if the client's TCP drops, the instructor's tile flickers and the student must restart the app entirely. Both visible to the student as a broken experience.

**Done when:**
- Server tile dot is **green** when frames received in last 3 s, **yellow** when 3 s < since-last-frame < 8 s (the grace window), **red** when ≥ 8 s.
- Client banner shows `Reconnecting… (Ns)` when `isConnected == false` AND session state != Waiting/Stopped.
- Client automatically re-handshakes the original token on reconnect — no user retype needed.
- Cached `examToken` survives reconnect (already partially true; verify).
- Event log records `student_disconnected{at=…}` and `student_reconnected{at=…, gap_seconds=…}` per drop.
- Acceptance test: kill the server process for 5 s while a student is mid-capture; restart; student's banner self-recovers within 2 s of server returning.

---

#### FR-3 — Instructor messaging is reliable, always renders, always logged

Messages from instructor to students must arrive within 1 s and be visible for 5 s, regardless of timing relative to session-state changes.

**Why:** This worked in some prior test, didn't work in the most recent test. Root cause was `BroadcastControl` filtering by state; v0.1.1 partially fixed via `BroadcastControlAll`. We need to *prove* it's reliable now via a unit test, not by re-testing manually.

**Done when:**
- Unit test in `server/server_test.go`: simulate a freshly-joined client (state ack not yet received). Server calls `sendMessage("all", "hi")`. Assert client receives the BroadcastMsg frame.
- Toast widget renders for exactly 5 s once `Set()` is called. Timer-backed `w.Invalidate()` ensures expiry triggers a redraw even when no other UI activity (today the UI might not re-render after 5 s).
- Eventlog records `message_broadcast{delivered_to=[<student IDs>], body=…}` so disputes can be audited.
- Acceptance test: click Send Message immediately after Start Exam (worst race). Toast appears on student within 1 s.

---

### v0.2.1 — "Multi-monitor + Diagnostics + Resilience"

#### FR-4 — Multi-display capture verified end-to-end

The v0.1.8 multi-display composite must actually display correctly on the teacher's tile.

**Why:** v0.1.8 added the capture-side composite, but the teacher's tile renders with `Fit.Contain` into a 16:9 box. A wide composite (e.g., 3840×1080) will letterbox heavily. Need to verify it doesn't look worse than single-display.

**Done when:**
- On a dual-monitor student machine, the teacher's tile shows BOTH monitors side-by-side in the captured image, no letterbox bar wider than 10% of tile height.
- The dashboard tile has a `Displays: 2` label under the student name when multi-display.
- Enlarged-view modal scales the composite proportionally with both monitors clearly visible.
- Linux X11 path also captures all displays (same composite logic as macOS).

---

#### FR-5 — Pre-exam system check (client)

Before the student clicks *Join*, the client runs a checklist and only allows Join when all checks pass.

**Why:** Today, capture permission failures, server unreachable, missing screenshot tool (Wayland) all surface as cryptic errors or silent black frames. Commercial tools have a "system check" screen.

**Done when:**
- The check **auto-runs** the moment the user clicks *Join* (no separate "Run Check" button). All checks run sequentially; the user sees progress: `Checking screen permission… ✓ / Checking server… ✓ / Validating token… ✓`.
- Checklist items:
  - ✓ Screen capture permission granted (calls `capture.New()` and `Capture()` once)
  - ✓ Server reachable (TCP dial test to discovered/manual address)
  - ✓ Token format valid (matches `[A-Z]{3}-[A-Z]{3}-[0-9]{3}` after trim/upper)
  - ✓ Wayland tool installed (Linux only)
- If any check fails, the form returns to the join view with a red error row above the form, including the failure reason and a "Retry" button.
- macOS-specific: if screen-capture permission is denied, the error row includes a clickable link that opens `x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture`.
- Successful check transitions directly to dashboard (no extra confirmation click needed).

---

#### FR-6 — Activity timeline view per student

The connect / disconnect events from FR-2 are surfaced in the dashboard UI as a per-student timeline. (The events themselves are written by FR-2; this FR is only the dashboard view + CSV inclusion.)

**Why:** Today, transient drops are visible to the instructor only as a flickering tile, and the FR-2 events are silently logged. Instructors need a way to see a student's connection history without opening the SQLite file.

**Done when:**
- Click on a student tile opens the existing enlarged-view modal. Beside the screen capture, add an "Activity" sidebar listing the last 10 events (`joined`, `disconnected (3s)`, `reconnected`, `message_received`, `viewed_by_instructor`).
- Each event shows wall-clock time and relative-to-now ("2 min ago").
- CSV export from v0.1.2 already exists; verify it includes the FR-2 events.
- The eventlog SQLite uses WAL mode so a server crash doesn't corrupt mid-write (see NFR-6, §9 risk 6).

---

#### FR-7 — Last-crash banner on relaunch

If the previous session crashed (`diag.LastCrash()` returns non-empty), the app shows a yellow banner on launch with a "Copy log path" button.

**Why:** Crashes today are invisible. Students don't know what happened. The `diag` package already writes a crash marker; we just need to surface it.

**Done when:**
- Both apps check `diag.LastCrash()` at startup.
- If non-empty: yellow banner at the top of the home screen, "Previous session ended unexpectedly at <time>. Logs at: <path>".
- "Copy log path" button copies the path to clipboard via the existing `CopyToClipboard`.
- Banner dismissed by user click; the crash marker file is renamed (`last-crash.txt` → `last-crash.txt.acknowledged-<timestamp>`) so it's preserved but not re-shown.

---

### v0.2.2 — "Polish + small UX wins"

#### FR-8 — Tile aspect respects source aspect ratio

The teacher's tile fits the captured image natively (no forced 16:9 with letterbox).

**Why:** v0.1.5 locked to 16:9 to fix tile sizing inconsistency. But for multi-monitor and ultra-wide sources, the locked aspect causes large letterbox bars. Better: tile takes the source's natural aspect.

**Done when:**
- `layoutStudentImage` returns natural dimensions (`width × scaled_image_height`).
- Outer grid layout is updated so cells in the same row share a uniform height = the tallest cell in that row. This avoids the v0.1.4 regression where mid-flight aspect changes caused "white space below image" by making each tile's height responsive to its own image — instead, the row sets a fixed height once per render and all tiles in the row honor it.
- Empty/disconnected tiles use a placeholder that defaults to 16:9 (so first-frame arrival doesn't reshape the whole row).
- The image is rendered with `Fit.Contain` semantics (letterbox bars if source aspect ≠ row aspect) so no cropping. Bars use the existing `placeholderBg` color.
- No visible white space below the image.

---

#### FR-9 — Confirm Stop Exam

A modal dialog confirms Stop Exam to prevent accidental clicks.

**Why:** Today Stop Exam is immediate. An accidental click ends the exam for all students. Add a confirm step.

**Done when:**
- Click Stop Exam → modal: "End exam for N students? This stops capture and cannot be undone."
- Default focus is "Cancel" (Esc dismisses).
- "End Exam" button uses the existing red destructive style.
- Confirming logs `exam_stopped{confirmed_by=instructor}`.

---

#### FR-10 — Student name on banner

The client's banner (`EXAM IN PROGRESS — Math 101`) also shows the student's name (`— as Yee Ling`).

**Why:** Confirms which student identity the client is operating as. Useful when multiple browser tabs / virtual desktops are open and the student wants to verify "yes this is me".

**Done when:**
- Banner reads `🔴 EXAM IN PROGRESS — <exam name> — as <student name>` (truncated for narrow windows).
- Sub-line includes the student name in the elapsed text: "Yee Ling · Xs elapsed".

---

#### FR-11 — Instructor "Kick student" action

Instructor can disconnect a misbehaving student from the dashboard.

**Why:** Currently no way to remove a student. Useful for disqualifying / removing a cheater without ending the whole exam.

**Done when:**
- Each student tile has a small "⋯" (kebab) button at the top-right of the card (next to the state badge). Click reveals a small menu with one option: "Kick from exam".
- (No right-click. Gio's right-click pointer event handling differs across platforms; an explicit button is more reliable.)
- Confirmation dialog: "Disconnect <name>? They cannot rejoin this exam with the current token."
- Server closes TCP connection AND adds the student ID to a per-exam blacklist that rejects token handshakes from that ID.
- Eventlog: `student_kicked{by=instructor, reason="manual"}`.
- Student client: banner switches to "Disconnected by instructor. Please contact the instructor."

---

#### FR-12 — Log paths in an About dialog

Both apps have an About dialog (accessible via menu) that shows version, build, log file paths, and a "Open log folder" button.

**Why:** Support questions today require me / you to know that logs live at `~/Library/Application Support/exam-monitor/`. Make this discoverable.

**Done when:**
- Menu item: Help → About `<app name>` (or app-specific keyboard shortcut).
- Dialog content: version + build + Go version + log path + log file size + "Open log folder" button (opens the directory in Finder/Explorer).
- "Copy diagnostics" button: zips the log file + crash markers + the latest eventlog SQLite into a `diagnostics-<timestamp>.zip` on the desktop.

---

## 5. Non-Functional Requirements

#### NFR-1 — No silent failures

Every error path either surfaces to the user (toast/banner/dialog) or is event-logged at WARN+. No `_ = err` or `// ignore` patterns. Lint rule: `errcheck` runs on every PR with no exclusions.

#### NFR-2 — Reproducible release pipeline

`git tag vX.Y.Z && git push --tags` is the only thing needed to publish. No manual notarization steps (notarization is deferred to v0.3.0).

#### NFR-3 — Cross-platform parity

Every FR works identically on macOS arm64, macOS amd64, Linux amd64, Windows amd64. Documented exceptions only (e.g., Wayland tool requirement, Linux multi-display deferred). FRs not yet implemented on a platform are blocked from that platform's release.

#### NFR-4 — Wire-protocol stability within v0.2.x

No breaking wire changes between v0.2.0 and v0.2.2. A v0.2.0 client must work with a v0.2.2 server, and vice versa. Any breaking change forces v0.3.0.

#### NFR-5 — Test coverage threshold

Every new FR ships with at least one test that exercises the wire-level OR state-machine behavior. UI layout code (Gio rendering) remains exempt — it can't be unit-tested without a display. Aim for **server top-level package** ≥ 30% coverage by end of v0.2.2 (currently 16.8 %).

#### NFR-6 — Verifiable acceptance

Every FR has a `Done when:` block listing 2–4 mechanically-checkable items. "Done" means all checks pass on the target platform — not "feels right."

#### NFR-7 — Performance budget

- Capture loop: ≤ 50 ms per frame on a 2024 MacBook Pro (currently ~20 ms).
- Encode + send: ≤ 100 ms per frame at JPEG Q=80, 1280px (currently ~40 ms).
- Server frame decode + dashboard refresh: ≤ 30 ms per student per frame.
- Frame rate: 4 FPS confirmed achievable on all target platforms.

---

## 6. Constraints

- **Language:** Go 1.25 (matches existing go.mod).
- **UI:** Gio v0.8.0 (matches existing dep).
- **Network:** LAN-only, no internet required for exam operation. Internet only used by FR-1's updater check (already in v0.1.6+).
- **Auth:** Token-based join, no user accounts.
- **Free + open-source:** MIT-licensed (once `LICENSE` file is added).
- **Binary size:** ≤ 20 MB per artifact (currently 10-19 MB).
- **Signing:** Currently unsigned. Code signing (Apple Developer ID + Authenticode) deferred to v0.3.0+.

---

## 7. Out of scope (deferred)

- **AI gaze tracking / cheating detection** — orthogonal to "monitor the screen."
- **Identity verification** (face recognition, ID upload) — out of scope.
- **Cloud-hosted server** — LAN-only architecture is intentional.
- **Mobile clients** (iPad / iPhone / Android) — desktop-only for now.
- **Browser-based clients** — desktop-only for now.
- **Webcam recording** — too privacy-sensitive for a free tool.
- **LMS integrations** (Canvas, Moodle, Blackboard) — not the target use case.
- **Linux Wayland native (no shell-out)** — deferred until PipeWire bindings mature.
- **Code signing** — deferred to v0.3.0+ when an Apple Developer account is in place.

---

## 8. Release roadmap

| Version | Items | Estimated effort | Trigger to next |
|---|---|---|---|
| **v0.2.0** | FR-1, FR-2, FR-3 | ~3 days (2026-05-19 → 2026-05-22) | All Done-When blocks for FR-1,2,3 verified manually on macOS + Windows. |
| **v0.2.1** | FR-4, FR-5, FR-6, FR-7 | ~3 days (2026-05-22 → 2026-05-25) | Same, for FR-4–7. |
| **v0.2.2** | FR-8, FR-9, FR-10, FR-11, FR-12 | ~2 days (2026-05-25 → 2026-05-27) | All 12 FRs verified. Tag "v0.2.x feature-complete." |
| **v0.3.0+** | Wire-protocol changes, code signing, Linux Wayland native | Out of scope of this doc |

---

## 9. Open questions & risks

1. **Token format collisions** — at the current `XXX-XXX-NNN` format with ~9M combinations and one exam per server, collision risk is negligible. But if we ever support multiple concurrent exams, the format may need lengthening. **Decision:** keep current format for v0.2.x; revisit in v0.3.x.

2. **mDNS reach across corporate / school networks** — mDNS doesn't traverse VLAN boundaries. Schools with VLAN-isolated student devices need manual IP entry. **Action:** the FR-5 system check should test mDNS reachability and recommend manual IP if it fails.

3. **Multi-display on macOS amd64 (Intel)** — v0.1.8 was tested on arm64 only. Need to verify Intel macOS also composites correctly. **Action:** part of FR-4 verification.

4. **Kicked student bypass via different student ID** — FR-11's blacklist keys on student ID. A determined cheater can simply re-join with a different ID. **Decision:** acceptable for v0.2.x; we're not a kiosk lockdown tool. Document in the spec.

5. **Performance on 20+ students** — never tested with > 2 students simultaneously. Bandwidth scaling: 4 FPS × ~100 KB × 20 = 8 MB/s server-side ingest. Should be fine on Ethernet, may be tight on shared WiFi. **Action:** measure during v0.2.2 sign-off; document the per-student bandwidth in the README.

6. **Crash recovery state** — if the server crashes mid-exam, the eventlog SQLite is interrupted. SQLite WAL mode would help. **Action:** confirm WAL mode is enabled in `eventlog.Open()` (or enable it) as a small NFR-6 addition.

---

## 10. References

- The 12 FRs are derived from the iterative session 2026-05-18, the user's enumerated complaints, and the commercial-proctoring research summarized in this session.
- Current implementation: `client/`, `server/` (Go 1.25, Gio v0.8.0).
- Upstream reference: `github.com/khayrultw/exam-monitor` — confirmed less feature-complete than current fork; not a target reference.
- Commercial tools surveyed: Respondus LockDown Browser, Proctorio, Honorlock, ExamSoft / Examplify, ProctorU, Pearson VUE.

---

*End of requirements document. Next step: invoke writing-plans skill to convert FRs to an executable implementation plan.*
