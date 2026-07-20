# ADR-0012: The Punakawan recorder is custom and injected visibly

## Status
Accepted

## Context
While Playwright control itself is reused (ADR-0011), Punakawan needs domain-specific behavior no off-the-shelf recorder provides: semantic locator priority, sensitive-input sanitization, an in-browser control overlay, and a visible recording indicator so users always know when their actions are being captured (§12 Playwright Human-Guided Flow Recorder; §15.5 Browser safety).

## Decision
The Punakawan recorder is custom and injected visibly.

## Consequences
Punakawan injects its own recorder script into the browser page, which visibly indicates active recording, exposes an overlay for pause/resume, marking fields secret, adding assertions/notes, and finishing the flow, and applies a strict semantic locator priority (test ID, role/accessible name, label, placeholder, stable text/attributes, scoped CSS, with XPath only as a diagnostic fallback) (§12.2, §12.3, §12.5, §12.7). This visibility and control design directly mitigates the risk of the recorder capturing sensitive data, through field classification, secret masking, visible recording controls, and approval requirements for existing-browser access (§12.6; §15.5; §24 Risk: Browser recorder captures sensitive data).
