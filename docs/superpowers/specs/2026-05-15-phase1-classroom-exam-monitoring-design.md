# Phase 1 — Classroom Exam Monitoring (B + D + G)

**Status:** Draft, awaiting user review
**Date:** 2026-05-15
**Project:** exam-monitor (Go modules: `client/`, `server/`)
**Authors:** Subaashan Nair (product), Claude Code (design partner)
**Predecessor work:** [Cross-platform stability fixes](../../../CLAUDE.md) (diag package, mDNS+manual-IP discovery, per-platform capture, macOS bundle script) — landed prior to this design.

---

## 1. Context

The repository at clone-time was a working tech demo: a Go desktop server an instructor runs in a classroom + a Go desktop client each student runs that screen-captures and TCP-streams JPEGs to the server. Three failure modes were reported in initial brainstorming: silent crashes, discovery fails, blank frames. Those were fixed in the predecessor work (panic recovery, multi-tier discovery, per-platform capture with macOS TCC handling).

What that fixed: the binary reliably connects and streams. What it left unfixed: there is **no exam concept** at all. The client streams continuously the moment it joins; the server has no Start/Stop, no lock-all, no message, no event log, no per-exam authentication, no retention policy, no audit trail of instructor actions. Beneficial for a tech demo, unfit for an exam.

This spec is **Phase 1 of a multi-phase plan** to turn the tool into something a school's exam monitor would actually pass an IT/privacy review. Phase 1 targets the three themes that turn the demo into a real exam tool:

- **Theme B — Exam-bounded capture.** Capture only between explicit Start and Stop; visible student-side notice; ≤24h frame/event retention enforced by code.
- **Theme D — Real instructor UI.** Mosaic with state badges; lock-all; per-student and class-wide messages; event-log CSV export. These are the features deep-research found instructors actually use ([NetSupport, Veyon, GoGuardian usage patterns](https://veyon.io/)).
- **Theme G — Session tokens.** A per-exam token plus the existing room number so a casual student on the LAN can't join an arbitrary room.

Themes deferred to later phases (decomposed during brainstorming):

| Phase | Theme | Why deferred |
|---|---|---|
| 2 | C — Network resilience (QR pairing, multi-VLAN handling) | mDNS already works for current dev scenario; QR is UX polish |
| 2 | F — Reliability extras (wake-lock, low-battery flagging) | Catch-ups, not load-bearing |
| 3 | A — Deployability (signed installers, MSI/PKG, DPA template) | Real deployment blocker, but doesn't gate design validation |
| 4 | E — Evidence-grade signals (window title, monitor count, process allow-list) | Mid-value features; build on top of Phase 1 |
| 5 | H — Compliance papers (DPIA template, retention policy doc) | Paper artefacts; no code dependency |

**Deployment topology (chosen during brainstorming):** school-wide LAN, multi-classroom, Windows / macOS / Linux student devices, each teacher runs their own server independently. Chromebook PWA companion is out of scope. Remote/at-home proctoring is out of scope (different product category).

---

## 2. Architecture

Phase 1 keeps every existing component, dependency, and capture pipeline. It adds three new concepts on top of today's wire protocol, additive only:

1. **Session state machine** on both client and server (`Idle/Waiting/Capturing/Locked/Stopped` on server; `Waiting/Capturing/Locked/Stopped` on client).
2. **Bidirectional control frames** — current protocol is client→server only; this adds server→client commands and client→server state acks, additive at new type IDs (10–29). Wire types 0/1/2 are unchanged.
3. **Persistent event log** on the server side, written to a per-exam SQLite file via the pure-Go [`modernc.org/sqlite`](https://gitlab.com/cznic/sqlite) driver, exportable as CSV.

```
                         ┌─────────────────────────────────┐
                         │           SERVER                │
                         │                                 │
                         │  ExamSession (NEW)              │
                         │  ├─ state                       │
                         │  ├─ exam_token (BIO-XQ7-394)    │
                         │  ├─ started_at / stopped_at     │
                         │  └─ retention_until             │
                         │                                 │
                         │  EventLog (NEW, SQLite)         │
                         │  └─ <date>_<exam-slug>.sqlite   │
                         │                                 │
                         │  Instructor UI (EXTENDED)       │
                         │  ├─ Start/Stop Exam buttons     │
                         │  ├─ Lock-all / Unlock           │
                         │  ├─ Message-one / -all          │
                         │  ├─ Tile state badges           │
                         │  ├─ Click-to-enlarge (audited)  │
                         │  └─ Export Event Log CSV        │
                         └────────────┬────────────────────┘
                                      │ TCP, HE-header
                                      │ types 0–2 unchanged
                                      │ types 10–29 NEW (control + ack)
                         ┌────────────┴────────────────────┐
                         │           CLIENT                │
                         │                                 │
                         │  SessionState (NEW)             │
                         │  Notice Banner (NEW)            │
                         │  Lock Overlay (NEW)             │
                         │  Message Toast (NEW)            │
                         │  Token field on JoinView (NEW)  │
                         └─────────────────────────────────┘
```

### 2.1 Protocol extension (server → client commands)

| Type | Name | Body |
|---|---|---|
| 10 | `EXAM_START` | `{exam_name, started_at_utc}` |
| 11 | `EXAM_STOP` | `{stopped_at_utc}` |
| 12 | `LOCK_SCREEN` | `{message_to_students, locked_by}` |
| 13 | `UNLOCK_SCREEN` | `{}` |
| 14 | `BROADCAST_MSG` | `{from, body, target_student_id_or_blank}` |
| 15 | `TOKEN_HANDSHAKE_OK` | `{exam_name, started: bool}` (sent in reply to client's handshake) |
| 16 | `TOKEN_HANDSHAKE_REJECT` | `{reason: "invalid"\|"expired"\|"room_mismatch"}` |

### 2.2 Protocol extension (client → server state ack)

| Type | Name | Body |
|---|---|---|
| 20 | `STATE_WAITING` | `{}` |
| 21 | `STATE_CAPTURING` | `{}` |
| 22 | `STATE_LOCKED` | `{}` |
| 23 | `STATE_STOPPED` | `{}` |
| 24 | `ACK_BROADCAST_MSG` | `{message_id}` |
| 25 | `TOKEN_HANDSHAKE` | `{token}` |

Existing wire types 0 (`NAME`), 1 (`MESSAGE`), 2 (`PICTURE`) are unchanged in semantics. `MESSAGE` (1) was previously a debug-only path that just logged; it is unused in Phase 1 (`BROADCAST_MSG` at type 14 replaces it for instructor messaging; the legacy type remains in the codepath for backward compatibility with the unmodified legacy code we already shipped).

Backward compatibility: old clients that never receive an `EXAM_START` will simply stay in their old "stream immediately" code path against a Phase 0 server. New clients connected to a Phase 1 server stay in `Waiting` until receiving `EXAM_START`. We deliberately do **not** auto-detect server version — the join screen requires a token field that older servers never validated, which functions as the implicit version gate.

---

## 3. Session model

### 3.1 Server state machine

```
   ┌─────┐                       teacher creates exam
   │Idle │ ──────────────────────────────────────────▶ ┌─────────┐
   └─────┘   (token generated,                         │Waiting  │
              SQLite file opened,                      │(room    │
              port still uses existing port logic)     │ open)   │
                                                       └────┬────┘
                                  teacher clicks Start      │
                                                            ▼
                                                  ┌──────────────┐
                                                  │  Capturing   │
                                                  └──┬───────┬───┘
                              teacher clicks Lock    │       │ teacher clicks Stop
                                                     ▼       ▼
                                              ┌──────────┐  ┌──────────┐
                                              │  Locked  │  │ Stopped  │
                                              └────┬─────┘  └──────────┘
                       teacher clicks Unlock       │
                                                   ▼
                                            (back to Capturing)
```

`Locked` is a **substate of Capturing**: frames still flow and instructors still see them; only the student-side UI changes. This lets an instructor freeze the room mid-exam without losing visibility.

### 3.2 Client state machine

```
  Connected
     ▼
  Token handshake ───reject──▶ Disconnected (with reason)
     │
     ▼ accept
  STATE_WAITING ─── EXAM_START ──▶ STATE_CAPTURING ─── LOCK ──▶ STATE_LOCKED
                                          ▲                       │
                                          └──────── UNLOCK ───────┘
                                          │
                                          │ EXAM_STOP
                                          ▼
                                    STATE_STOPPED
                                    (banner gone,
                                     capturer.Close(),
                                     AlwaysOnTop(false))
```

### 3.3 Lifecycle behaviour (the non-obvious cases)

| Event | Server | Client |
|---|---|---|
| Student joins before Start | `student_joined` logged. Stays in `Waiting`. **No frames captured.** | Banner: "Waiting for exam to begin." Capturer not initialised. |
| Teacher clicks Start | `exam_started` logged. Broadcasts `EXAM_START` to all `Waiting` clients. State → `Capturing`. | Capturer initialised. First frame sent. Banner: "Monitoring active — \<name>". Window forced AlwaysOnTop. |
| Student joins after Start | `student_joined` with `late_join: true`. Immediately sends `EXAM_START`. | Same as a normal Start. |
| Teacher clicks Lock All | `lock_applied` logged. Broadcasts `LOCK_SCREEN`. State → `Locked` (substate). | Render fullscreen black Gio window (separate from main window). Capturer keeps running. |
| Teacher clicks Unlock | `lock_released` logged. Broadcasts `UNLOCK_SCREEN`. State → `Capturing`. | Close overlay window. |
| Teacher sends message | `message_broadcast` logged. Sends `BROADCAST_MSG`. | 5s toast below banner. Queued if multiple. |
| Teacher clicks Stop | `exam_stopped` logged. Broadcasts `EXAM_STOP`. State → `Stopped`. Sets `retention_until_utc = now + 24h`. | Capturer.Close(). Banner gone. Window AlwaysOnTop released. Late frames after Stop are rejected by server. |
| Client dies mid-exam | After 5s grace (existing logic), `client_offline` logged. Tile shows red badge. | n/a |
| Client reconnects mid-exam | If state is `Capturing`, server immediately sends `EXAM_START` to re-sync. `client_reconnected` logged. | Re-init capturer, resume streaming. |

### 3.4 Retention policy (mechanical)

- During `Capturing` / `Locked`: frames held in RAM only. Not written to disk.
- On `Stopped`: keep the last frame of each student as a small final-image snapshot, written to `<UserConfigDir>/exam-monitor/exams/<date>_<slug>/finals/<student-id>.jpg`. Discard all other frames from RAM. Final snapshots inherit the same 24h retention as the SQLite file.
- 24h after `Stopped`: a reaper goroutine deletes the SQLite file and the `finals/` directory.

This is the **moat from research finding (B)**: capture is exam-bounded, frames are not persisted by default, and an audit log of every instructor action exists.

---

## 4. Instructor UI

### 4.1 Create-Exam screen (replaces today's room-number-only Home)

```
┌─────────────────────────────────────────────────────────┐
│  Exam Monitor — Teacher                                 │
├─────────────────────────────────────────────────────────┤
│  Exam name:    [Biology Final Term 1                  ] │
│  Room number:  [123456                                ] │
│  Token:        BIO-XQ7-394                  [regenerate]│
│                                                         │
│   Share with students:                                  │
│     Room number  123456                                 │
│     Exam token   BIO-XQ7-394                            │
│   (Project this screen so students can copy the values)│
│                                                         │
│                                       [Open Waiting Room]│
└─────────────────────────────────────────────────────────┘
```

QR rendering is Phase 2 (Theme C). Phase 1 shows token + room as text only.

### 4.2 Dashboard (the existing mosaic, with a new top control bar)

The mosaic stays as-is. We add a top control bar that's **context-aware by session state**:

| State | Top bar shows |
|---|---|
| `Waiting` | "Waiting for students" + count + `[Start Exam]` (disabled until ≥1 student joined) |
| `Capturing` | exam clock (mm:ss elapsed) + `[Lock All]` `[Send Message]` `[Stop Exam]` |
| `Locked` | red "ROOM LOCKED" indicator + `[Unlock]` `[Stop Exam]` |
| `Stopped` | "Exam ended" + `[Export Event Log]` `[Close]` |

### 4.3 Tile state badges

Each student tile gets a top-right badge. Colour-coded *and* glyph-labelled (accessibility — color-blind users can't rely on hue alone):

| Badge | Meaning |
|---|---|
| 🟢 `W` | Waiting (in waiting room, not capturing yet) |
| 🟢 `●` | Capturing OK (frames arriving) |
| 🔴 `!` | Offline (no frames > 5s, within grace period) |
| ⚫ `🔒` | Locked (overlay applied) |
| 🟡 `?` | Connecting / reconnecting |

State transitions per tile are written to the event log; a post-exam reviewer can answer "when did this student go offline?"

### 4.4 Click-to-enlarge

Already exists in [server/viewer.go](../../../server/viewer.go). Phase 1 extends it to log an `instructor_viewed_student` event with `(student_id, viewed_at_utc, viewed_until_at_utc)`. This is the **audit log of teacher action** the privacy moat requires.

### 4.5 Lock-all dialog

```
┌───────────────────────────────────────┐
│  Lock all student screens?            │
│                                       │
│  Message shown to students:           │
│  [Please stop typing until further    │
│   instructions.                     ] │
│                                       │
│              [Cancel]  [Lock All]     │
└───────────────────────────────────────┘
```

### 4.6 Send-message dialog

```
┌────────────────────────────────────────────────┐
│  Send message                                  │
│                                                │
│  ○ To all students                             │
│  ● To selected student: [Aisha Rahman    ▾]    │
│                                                │
│  Message:                                      │
│  [Please raise your hand when you're done.  ]  │
│                                                │
│                          [Cancel]  [Send]      │
└────────────────────────────────────────────────┘
```

### 4.7 Event-log CSV export

Single button on the `Stopped` screen. Writes a CSV with one row per event, ordered by `timestamp_utc`, flattening `details_json` into a single cell. Saved to `<UserConfigDir>/exam-monitor/exports/<exam-slug>-<date>.csv`. Header columns: `timestamp_utc, event_type, student_id, student_name, details`.

---

## 5. Student-side UI

### 5.1 Persistent notice banner

Visible whenever client state is `Capturing` or `Locked`. Bound to the session state machine. Top strip of the main client window.

The window is **set to always-on-top while in `Capturing` / `Locked`**. Implementation approach (best-effort, in this order):

1. If Gio exposes an always-on-top window option in the version we use, use it.
2. Otherwise, call a small platform-specific helper:
   - **macOS:** `objc_msgSend` `setLevel:NSFloatingWindowLevel` via cgo bridge (~20 LOC, only needed if Gio doesn't already wrap it).
   - **Windows:** `SetWindowPos` with `HWND_TOPMOST` via the `golang.org/x/sys/windows` package or existing `lxn/win` dep (~10 LOC).
   - **Linux X11:** set `_NET_WM_STATE_ABOVE` on the window via `jezek/xgb` (already a transitive dep, ~20 LOC).
   - **Linux Wayland:** the compositor decides; many Wayland compositors will reject. Document as soft-best-effort.
3. If all of the above fail (or for Wayland fallback), accept the limitation and surface it: the dashboard tile for that student shows a `🪟 not-on-top` warning badge so the instructor can follow up verbally.

This is the only place in Phase 1 where we touch platform-specific window-manager APIs. Each helper is small (< 50 LOC) and lives behind a `setAlwaysOnTop(w *app.Window, on bool) error` interface that returns `ErrNotSupported` rather than failing hard.

```
┌─────────────────────────────────────────────────────────┐
│  🔴 MONITORING ACTIVE — Biology Final Term 1            │
│     Instructor: Ms. Lim · 12:34 elapsed                 │
└─────────────────────────────────────────────────────────┘
```

The window can still be minimised or moved by the student; it cannot be made not-on-top while capture is active. On `EXAM_STOP`, we restore `AlwaysOnTop(false)`.

### 5.2 Lock overlay

Separate, full-screen, un-closeable Gio window that appears when client state is `Locked`. Properties:

- Full-screen via `app.Fullscreen` window option.
- Always-on-top via the helper from §5.1 (with the same per-platform fallback path; if always-on-top fails, the overlay is still visible but a determined user can alt-tab past it more easily).
- Ignores `KeyEvent` and `MouseEvent` from the overlay window itself.
- No close affordance — only path out is `UNLOCK_SCREEN` from the server.

```
┌─────────────────────────────────────────────────────────┐
│                                                         │
│                       🔒                                │
│                                                         │
│                EXAM PAUSED                              │
│                                                         │
│       Please stop typing until further                  │
│       instructions from your instructor.                │
│                                                         │
│             — Ms. Lim, 12:36                            │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

**Honest caveat documented in user-facing release notes:** a determined student can `Cmd+Tab` / `Alt+Tab` past it, or kill the process from a terminal. The lock is a "please stop" signal, not a hard freeze. A hard freeze would require kernel hooks; we explicitly chose not to build them (research finding §3). The event log records every lock application; subsequent activity from a locked client is auditable post-exam.

### 5.3 Message toast

On `BROADCAST_MSG`, slide in from top below the banner, auto-dismiss after 5 seconds.

```
┌─────────────────────────────────────────────────────────┐
│  🔴 MONITORING ACTIVE — Biology Final Term 1            │
├─────────────────────────────────────────────────────────┤
│  💬 Ms. Lim: Please raise your hand when you're done.   │
│                                       [Dismiss]         │
└─────────────────────────────────────────────────────────┘
```

Multiple messages within 5 s queue. The last-seen message is also kept in a small "Recent messages" pane in the main window so blinked-past messages are not lost.

### 5.4 Waiting-room view

Before `EXAM_START`:

```
┌─────────────────────────────────────────────────────────┐
│  Exam Monitor — Student                                 │
│                                                         │
│  Connected: Aisha Rahman (S001)                         │
│                                                         │
│  Waiting for your instructor to start the exam...       │
│                                                         │
│  ⏱ joined 0:32 ago                                     │
│                                                         │
│  [Leave]                                                │
└─────────────────────────────────────────────────────────┘
```

The Leave button sends a clean disconnect; logs `student_left_voluntary`.

### 5.5 Stopped view

```
┌─────────────────────────────────────────────────────────┐
│  Exam Monitor — Student                                 │
│                                                         │
│  ✓ Exam ended.                                          │
│  Your screen is no longer being captured.               │
│                                                         │
│                                          [Close]        │
└─────────────────────────────────────────────────────────┘
```

### 5.6 State ↔ visual summary

| Session state | What student sees |
|---|---|
| `Waiting` | Calm "waiting" pane. No banner. No capture. |
| `Capturing` | Red banner + main window forced AlwaysOnTop + capturer streaming |
| `Locked` | All of the above + black full-screen overlay on top |
| `Stopped` | "Exam ended" pane. Banner gone. AlwaysOnTop released. Capturer closed. |
| Disconnected (any state) | "Reconnecting..." with last-error string |

---

## 6. Session tokens + state-mismatch handling

### 6.1 Token format

```
  token = <3 letters>-<3 letters>-<3 digits>
  e.g.  BIO-XQ7-394
```

- Filter the alphabet to avoid ambiguous glyphs: drop `O`, `I`, `L`. Remaining alphabet: 23 letters. Digits stay full 0–9.
- Total entropy: 23³ × 23³ × 10³ ≈ 1.48 × 10¹¹ combinations (~148 billion). Sufficient for a 30–90 minute exam window against a casual LAN attacker. **We are explicitly not building a cryptographic auth system in Phase 1**; this is "low-friction defence against the curious," not "defence against an attacker."
- Default `valid_until = generated_at + 4h`. After Stop, the token is invalidated; after `valid_until`, the server refuses new joins.

### 6.2 Join flow with token

```
Student fills JoinView:
  Student ID:   [S001        ]
  Name:         [Aisha Rahman]
  Room number:  [123456      ]
  Exam token:   [BIO-XQ7-394 ]  ← NEW
  Server IP:    [optional    ]  (existing)
       ↓
  Discover server (existing chain: manual → mDNS → broadcast)
       ↓
  TCP connect to room port
       ↓
  Send TOKEN_HANDSHAKE (new type 25) with token bytes
       ↓
  Server validates:
       ├─ Token unknown      → close, log invalid_token
       ├─ Token expired      → close, log expired_token
       ├─ Token belongs to a different room → close, log room_mismatch
       └─ Token OK           → reply TOKEN_HANDSHAKE_OK (type 15)
       ↓
  Send NAME (existing type 0)
       ↓
  Enter Waiting state — no capture until EXAM_START
```

### 6.3 Token persistence on the client

The token field is **not** persisted across sessions (unlike Student ID, Name, Room, Server IP — those stay via the existing `FormData`). Rationale: a single token is bound to one exam; persisting it after the exam ends invites confusion. Cleared on `EXAM_STOP` and on app restart.

### 6.4 Token normalisation and storage form

- **Canonical form (used for storage and comparison)**: uppercase, alphanumeric only, no hyphens. E.g. `BIOXQ7394`.
- **Display form (used in the Create-Exam screen and all student-facing prompts)**: hyphenated triplets. E.g. `BIO-XQ7-394`.
- Server normalises input on every comparison: trim, uppercase, strip non-alphanumeric. So `bio-xq7-394`, `BIOXQ7394`, `Bio Xq7 394` all map to `BIOXQ7394`.

### 6.5 State-mismatch handling

| Mismatch | Cause | Recovery |
|---|---|---|
| Client receives `LOCK_SCREEN` but is in `Waiting` | Missed `EXAM_START` (e.g. reconnect race) | Treat as `EXAM_START` first, then apply lock. Log `state_recovered`. |
| Client receives `EXAM_STOP` but is `Locked` | Teacher stopped without unlocking first | Tear down lock overlay, transition to `Stopped`. Log `force_unlock_on_stop`. |
| Client sends frames but server is `Waiting` | Client lost the `EXAM_STOP`, kept streaming | Server rejects frames with `STATE_REJECTED`; client interprets as "stop streaming." Log `late_frame_rejected`. |
| Server gets `STATE_CAPTURING` from a token-less connection | Impossible per handshake, but defensive | Close connection, log `unauthenticated_state_claim`. |

All four are recorded so post-exam review can spot weird network behaviour.

### 6.6 Explicit non-goals for Phase 1

- **No TLS.** Plaintext frames stay on the wire. Theme A/H concern, deferred.
- **No HMAC-signed control frames.** A MITM on the LAN can spoof `LOCK_SCREEN` to a client. Accepted; mitigating requires either TLS or a pre-shared secret, both bigger than Phase 1.
- **No frame-rate limiting beyond today's 5 MB-per-frame cap and 10s read timeout.**
- **No federated admin / IT central console.** "School-wide" deployment in Phase 1 means each teacher runs an independent server; no federation. Phase 2+ is when a central console becomes worth building once we have evidence about what cross-classroom views matter.

---

## 7. Event log schema + retention enforcement

### 7.1 Storage

Pure-Go SQLite via [`modernc.org/sqlite`](https://gitlab.com/cznic/sqlite). No CGO. Cross-compiles cleanly to Linux/Windows/macOS without per-platform toolchain pain.

One **directory** per exam at `<UserConfigDir>/exam-monitor/exams/<ISO-date>_<exam-name-slug>/`, containing:

- `events.sqlite` — the event log file.
- `finals/<student-id>.jpg` — the final-frame snapshots from §3.4.

Slug is lowercase, alphanumeric + hyphens, max 32 chars (e.g., "Biology Final Term 1" → `biology-final-term-1`). ISO date is `YYYY-MM-DD`. One-directory-per-exam keeps retention enforcement trivial — `os.RemoveAll` of one directory after 24h, not "delete rows older than X" across a shared store.

### 7.2 Schema

```sql
CREATE TABLE events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp_utc   TEXT NOT NULL,           -- ISO 8601
    event_type      TEXT NOT NULL,
    student_id      TEXT,
    student_name    TEXT,                    -- denormalised for export readability
    details_json    TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX idx_events_ts ON events (timestamp_utc);
CREATE INDEX idx_events_student ON events (student_id);

CREATE TABLE exam_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
-- rows: exam_name, room_port, token, started_at_utc, stopped_at_utc, retention_until_utc
```

### 7.3 Phase 1 event types

| Event | Trigger | `details_json` |
|---|---|---|
| `exam_created` | Server boots into Waiting | `{token, port}` |
| `student_joined` | First NAME frame received | `{remote_addr, late_join: bool}` |
| `student_left_voluntary` | Clean disconnect | `{}` |
| `client_offline` | Grace-period removal | `{offline_duration_seconds}` |
| `client_reconnected` | Reconnect within grace | `{}` |
| `exam_started` | Teacher clicks Start | `{}` |
| `exam_stopped` | Teacher clicks Stop | `{}` |
| `lock_applied` | Teacher clicks Lock All | `{message_to_students}` |
| `lock_released` | Teacher clicks Unlock | `{}` |
| `message_broadcast` | Teacher clicks Send Message | `{to: "all"\|student_id, body}` |
| `instructor_viewed_student` | Teacher clicks tile to enlarge | `{viewed_at_utc, viewed_until_at_utc}` |
| `invalid_token` | Failed token handshake | `{remote_addr, attempted_token_redacted}` |
| `late_frame_rejected` | State-mismatch defensive reject | `{client_state, server_state}` |
| `state_recovered` | Client recovered from desync | `{from_state, to_state}` |

The `instructor_viewed_student` event is the single most important entry for compliance — it lets a school's privacy officer audit teacher behaviour.

### 7.4 Retention enforcement

A single reaper goroutine on `Server.Start` runs hourly. Pseudocode:

```go
func (s *Server) reapExpiredExams(examsRoot string) {
    ticker := time.NewTicker(1 * time.Hour)
    defer ticker.Stop()
    for range ticker.C {
        entries, _ := os.ReadDir(examsRoot)
        for _, e := range entries {
            if !e.IsDir() { continue }
            examDir := filepath.Join(examsRoot, e.Name())
            until, ok := readRetentionUntil(filepath.Join(examDir, "events.sqlite"))
            if ok && time.Now().UTC().After(until) {
                if err := os.RemoveAll(examDir); err != nil {
                    slog.Warn("reap failed", "dir", examDir, "err", err)
                    continue
                }
                slog.Info("exam log purged", "dir", e.Name())
            }
        }
    }
}
```

`readRetentionUntil` opens the SQLite file read-only, reads `exam_meta.retention_until_utc`, closes the file, and returns the parsed timestamp. The reaper also runs once at startup to catch any exam whose retention expired while the server was down.

This is the **mechanical enforcement** of the moat. Without it, "frames retained ≤24h" is a promise; with it, it's code.

### 7.5 CSV export

Pull every row ordered by `timestamp_utc`, flatten `details_json` into one cell. Header row matches schema columns plus `details`. Saved to `<UserConfigDir>/exam-monitor/exports/<exam-slug>-<date>.csv`. Button enabled only in the `Stopped` state.

---

## 8. File-level plan

### 8.1 New files / packages

```
server/
  session/
    session.go              # ExamSession struct, state machine, transitions
    session_test.go         # table-driven state machine tests
    token.go                # token generation + normalisation + expiry check
    token_test.go           # generation + roundtrip + normalisation tests
  eventlog/
    eventlog.go             # SQLite open, schema migration, Record, Query
    eventlog_test.go        # round-trip + index tests
    export.go               # CSV export
    export_test.go          # header + ordering tests
    reaper.go               # retention reaper goroutine
    reaper_test.go          # reaper with seeded retention timestamps
  controlframe.go           # new wire types 10–29 encode/decode
  controlframe_test.go      # round-trip per type
client/
  session/
    session.go              # client state machine
    session_test.go         # transitions per server-frame
  ui/
    banner.go               # notice banner widget
    overlay.go              # full-screen lock overlay (separate window)
    toast.go                # message toast
tests/
  e2e/
    e2e_test.go             # full end-to-end on 127.0.0.1
```

### 8.2 Modified files

```
server/
  server.go                 # bidirectional control frame routing
  main.go                   # init session, eventlog, reaper
  home.go                   # Create Exam screen (exam name + token display)
  dashboard.go              # context-aware top bar + dialogs
  viewer.go                 # add audit log on enlarge
  student.go                # add session state field
  student_manager.go        # state badge query
client/
  client.go                 # capture gated on session state, control-frame router
  main.go                   # init client session, wire to views
  joinView.go               # add Exam token field + validation
  persistence.go            # (no change — token NOT persisted)
  dashboard.go              # bind to client session state
  views.go                  # state-aware view routing
```

### 8.3 New dependencies

- `modernc.org/sqlite` — pure-Go SQLite driver, ~3 MB binary impact.

No other new deps. mDNS (`grandcat/zeroconf`) and slog stay from prior phase.

---

## 9. Testing strategy

| Package | Tests |
|---|---|
| `server/session` | `TestSessionStateMachine_ValidTransitions` (table over every legal transition); `TestSessionStateMachine_RejectsInvalid` (no Stop→Capturing, no Capturing→Idle); `TestSessionToken_GenerateRoundtrip`; `TestSessionToken_Normalisation` (mixed case, with/without hyphens, with spaces); `TestSessionToken_ExpiryEnforcement` |
| `server/eventlog` | `TestOpenCreateSchema` (open new file, verify tables); `TestRecord_RoundtripJSON`; `TestExportCSV_HeaderAndOrdering`; `TestReap_OnlyDeletesExpired` (table over retention_until past/future/missing) |
| `server` | `TestHandleControlFrame_LockBroadcastsToAllCapturing`; `TestHandleControlFrame_RejectFromUnauthenticated`; `TestLateJoiner_GetsExamStartImmediately` |
| `client/session` | `TestClientStateMachine_HandlesEachServerFrame` (table); `TestClientStateMachine_RecoversFromDesync` (each of the four mismatch cases) |
| `client/capture` | Existing tests stand. Add `TestCapturer_NotInitialisedInWaitingState` — capture must not start until `EXAM_START` received. |
| `tests/e2e` | `TestEndToEnd_127001_WaitingToCapturingToStopped` — Server and one fake-capturer Client over `127.0.0.1`; assert: no frames before Start; ≥ 1 frame between Start and Stop; 0 frames after Stop; event log has `exam_started`, `student_joined`, `exam_stopped` in order. |

Coverage targets: ≥ 80 % on every new package. Top-level `client`/`server` packages stay at low coverage (Gio UI glue not unit-testable without a display). Race detector enabled (`go test -race`).

---

## 10. Done definition

1. A teacher can start a server, create an exam ("Biology Final"), see a token, students join with the token + room number, students wait, teacher clicks Start, capture begins on all clients with visible banner, teacher locks the room, sends a message, unlocks, stops the exam, exports a CSV — **end-to-end on local hardware, demoable**.
2. **No frames are captured** at any time outside `Capturing` or `Locked` state (auditable via the e2e test and the absence of writes to the `frames` directory before Start / after Stop).
3. **Every instructor action** (start, stop, lock, unlock, message, view-tile) is recorded in the event log with a UTC timestamp and instructor identity.
4. **24h after Stop**, the SQLite file and `finals/` directory for that exam are deleted by the reaper (verified by `TestReap_OnlyDeletesExpired`).
5. All tests pass with `go test -race -cover ./...` on both modules; new packages ≥ 80 % coverage; e2e test passes on `127.0.0.1`.
6. Native builds work on macOS; cross-compile-friendly subpackages (`session`, `eventlog`, `capture/*backend`, `internal/diag`) still cross-compile cleanly to Linux/Windows.

## 11. Risks and unknowns

| Risk | Mitigation |
|---|---|
| Gio `app.AlwaysOnTop` may not work on Wayland | Detect Wayland session at runtime; fall back to a soft "please keep this window visible" banner with no AlwaysOnTop. Logged so the instructor sees it on the tile. |
| Gio fullscreen overlay on Wayland is compositor-dependent | Same fallback as above; the soft lock still displays the message; the instructor sees `lock_applied_soft` in the log so they know to follow up verbally. |
| SQLite via modernc.org/sqlite is slower than CGO-based mattn/go-sqlite3 | Acceptable — Phase 1 event log writes are < 100/exam; performance is irrelevant at this scale. |
| Students can `Cmd+Tab` past the lock overlay | Accepted; documented in release notes. The event log records every lock; subsequent activity is auditable. |
| Wire-protocol additions interact poorly with the existing `default:` decode-as-picture in `handleStudent` | Patch `handleStudent` to explicitly reject types ≥ 10 unless they're known state acks. Add a test. |

## 12. Estimated effort

| Slice | Days |
|---|---|
| `server/session` state machine + token + tests | 2 |
| `server/eventlog` SQLite + CSV + reaper + tests | 2 |
| `server/server` new wire types + bidirectional control + state-aware routing | 2 |
| `server` dashboard new buttons + dialogs | 2 |
| `client/session` state machine + tests | 1 |
| `client/capture` gating on session state | 0.5 |
| `client` notice banner + AlwaysOnTop toggle | 1 |
| `client` lock overlay (new full-screen Gio window) | 1.5 |
| `client` message toast | 0.5 |
| `client` join screen + token field + state routing | 1 |
| `tests/e2e` integration test | 1 |
| Buffer / unknowns | 1.5 |
| **Total** | **~16 days for one engineer** |

---

## 13. References

- [Veyon project](https://veyon.io/) — actively maintained open-source classroom monitoring; closest comparable.
- [NetSupport School](https://www.netsupportschool.com/) — commercial reference for instructor UI patterns.
- [EFF student privacy](https://www.eff.org/issues/student-privacy) — privacy/ethics norms for educational monitoring.
- [CDT student monitoring reports](https://cdt.org/insights/report-hidden-harms-the-misleading-promise-of-monitoring-students-online/) — teacher misuse evidence; supports audit-log moat.
- [FERPA at 34 CFR Part 99](https://studentprivacy.ed.gov/) — US education records law.
- [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) — chosen SQLite driver.
- Predecessor design: cross-platform stability (see [CLAUDE.md](../../../CLAUDE.md)).
