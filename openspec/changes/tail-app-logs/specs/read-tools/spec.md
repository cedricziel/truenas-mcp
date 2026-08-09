## ADDED Requirements

### Requirement: Serve a read operation from an event source where the target exposes no method

A read operation MAY be served by a middleware event source rather than a method, where the target exposes the capability only that way. Such an operation SHALL present the same interface as any other read operation: an `op` on a concern tool, with the same annotations and the same result shaping.

A caller SHALL NOT need to know which of the two a given operation uses.

#### Scenario: Operation backed by an event source

- **WHEN** a caller invokes a read operation whose data comes from an event source
- **THEN** the operation returns the data in the same shape as any other read operation
- **AND** the tool carries the same read-only annotation

#### Scenario: Event source refuses the request

- **WHEN** the event source refuses the request because its preconditions are unmet
- **THEN** the server returns the target's refusal
- **AND** the error is distinguishable from an operation that legitimately returned nothing

### Requirement: Bound a text-producing operation by size as well as by count

An operation returning lines of free text SHALL bound its result by total size in addition to any count, and SHALL report which bound it applied.

A count alone does not bound a response: one line of output can be arbitrarily long, so a limit expressed only in lines permits an unbounded result. This is the same contract the item-count bound already keeps — a truncated answer must never read as a complete one — extended to the axis along which text actually grows.

#### Scenario: Requested count reached before the size bound

- **WHEN** the requested number of lines is returned without exceeding the size bound
- **THEN** the response contains that many lines
- **AND** the response indicates the result was bounded by count

#### Scenario: Size bound reached before the requested count

- **WHEN** the accumulated text reaches the size bound before the requested number of lines is collected
- **THEN** the response contains the lines gathered so far
- **AND** the response indicates the result was bounded by size rather than by count

#### Scenario: A single line exceeds the size bound

- **WHEN** one line on its own exceeds the size bound
- **THEN** the response does not grow without limit
- **AND** the response states that content was withheld

### Requirement: Return log content with the timestamps the target supplied

An operation returning log output SHALL return each entry's timestamp alongside its content where the target supplies one.

The server SHALL NOT maintain per-caller position state across calls. A caller that calls again receives the current tail, and the timestamps are what let it recognize the entries it has already seen.

#### Scenario: Entries carry timestamps

- **WHEN** log entries are returned and the target supplied timestamps
- **THEN** each entry's timestamp accompanies its content

#### Scenario: Calling again

- **WHEN** a caller invokes the operation a second time
- **THEN** the response is the current tail rather than only what is new since the previous call
- **AND** the server holds no cursor on the caller's behalf
