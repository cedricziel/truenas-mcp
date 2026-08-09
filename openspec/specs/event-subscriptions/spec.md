## Purpose

Lets the server consume TrueNAS middleware event sources, which deliver data the
target pushes over an established connection rather than returning it from a
request, so capabilities exposed only as event sources are reachable.

## Requirements

### Requirement: Subscribe to an event source and receive what it emits

The server SHALL be able to open a subscription to a named middleware event
source, supplying that source's arguments, and SHALL deliver emitted events to
the caller that opened it. The server SHALL correlate each event with its
subscription and SHALL NOT deliver it to another caller.

#### Scenario: Event source emits while a subscription is open

- **WHEN** an event source emits an event while a caller's subscription is open
- **THEN** the event is delivered to that caller with its payload preserved

#### Scenario: Two subscriptions open at once

- **WHEN** two subscriptions to different event sources are open on one connection
- **THEN** each caller receives only events belonging to its own subscription

#### Scenario: Unsolicited message belongs to no subscription

- **WHEN** the target sends a message that matches no open subscription
- **THEN** the server discards it and no caller is affected

### Requirement: End a subscription when the caller stops consuming it

The server SHALL close a subscription when the work that opened it finishes, is
cancelled, or reaches its bound, and SHALL release associated resources. An
indefinite event source SHALL NOT keep producing for a caller that is no longer
reading on the shared connection.

#### Scenario: Consumer reaches its bound

- **WHEN** a caller has taken as much as it asked for
- **THEN** the server ends the subscription and the target stops sending events

#### Scenario: Caller cancels before the source finishes

- **WHEN** the caller's context is cancelled while events are still arriving
- **THEN** the server ends the subscription and returns without waiting for the source

### Requirement: Fail a subscription rather than hang when the connection drops

If the target connection is lost while a subscription is open, the server SHALL
fail the subscription with an interruption error rather than leave its consumer
waiting. The server SHALL NOT silently re-establish it on reconnect.

#### Scenario: Connection lost mid-stream

- **WHEN** the connection drops while a subscription is open
- **THEN** its consumer is released with an interruption error

#### Scenario: Reconnect does not silently resume

- **WHEN** the client reconnects after a drop that interrupted a subscription
- **THEN** the interrupted subscription is not resumed

### Requirement: Report a subscription refused by the target distinctly

When the target refuses a subscription because its arguments, preconditions, or
caller role are invalid, the server SHALL return the target's refusal rather
than an empty result.

#### Scenario: Event source refuses its arguments

- **WHEN** a subscription is opened with arguments the event source rejects
- **THEN** the server returns the target's error distinctly from no events
