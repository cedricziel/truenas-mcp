## Purpose

Lets the server consume TrueNAS middleware event sources, which deliver data the target pushes over an established connection rather than returning it from a request, so that capabilities exposed only as event sources — container logs among them — are reachable at all.

## ADDED Requirements

### Requirement: Subscribe to an event source and receive what it emits

The server SHALL be able to open a subscription to a named middleware event source, supplying that source's arguments, and SHALL deliver the events it emits to the caller that opened it.

Events arrive unsolicited on the shared connection rather than as a reply to a request, so the server SHALL correlate each event with the subscription that asked for it and SHALL NOT deliver a subscription's events to any other caller.

#### Scenario: Event source emits while a subscription is open

- **WHEN** an event source emits an event while a caller's subscription is open
- **THEN** the event is delivered to that caller
- **AND** the event's payload is preserved as the source sent it

#### Scenario: Two subscriptions open at once

- **WHEN** two subscriptions to different event sources are open on the same connection
- **THEN** each caller receives only the events belonging to its own subscription

#### Scenario: Unsolicited message that belongs to no subscription

- **WHEN** the target sends a message that matches no open subscription
- **THEN** the server discards it
- **AND** no caller is affected

### Requirement: End a subscription when the caller stops consuming it

The server SHALL close a subscription when the work that opened it finishes, is cancelled, or reaches its bound, and SHALL release the resources associated with it.

An event source that streams indefinitely SHALL NOT keep producing for a caller that is no longer reading, since the connection is shared and an abandoned subscription would deliver events for the lifetime of the process.

#### Scenario: Consumer reaches its bound

- **WHEN** a caller has taken as much as it asked for
- **THEN** the server ends the subscription
- **AND** the target stops sending events for it

#### Scenario: Caller cancels before the source finishes

- **WHEN** the caller's context is cancelled while events are still arriving
- **THEN** the server ends the subscription
- **AND** the call returns without waiting for the source to finish

### Requirement: Fail a subscription rather than hang when the connection drops

If the connection to the target is lost while a subscription is open, the server SHALL fail that subscription with an error identifying the interruption, rather than leaving its consumer waiting.

A subscription SHALL NOT be silently re-established on reconnect, because the consumer cannot tell how much it missed while the connection was down and a resumed stream would read as a continuous one.

#### Scenario: Connection lost mid-stream

- **WHEN** the connection drops while a subscription is open
- **THEN** the subscription's consumer is released with an error
- **AND** the error identifies the connection as interrupted rather than reporting an empty result

#### Scenario: Reconnect does not silently resume

- **WHEN** the client reconnects after a drop that interrupted a subscription
- **THEN** the interrupted subscription is not resumed
- **AND** a caller that wants more opens a new one

### Requirement: Report a subscription refused by the target distinctly

When the target refuses a subscription — because its arguments are invalid, its preconditions are unmet, or the caller's credential lacks the role it requires — the server SHALL return the target's refusal rather than an empty result.

#### Scenario: Event source refuses its arguments

- **WHEN** a subscription is opened with arguments the event source rejects
- **THEN** the server returns the target's error
- **AND** the error is identifiable as the target's rather than as an absence of events
