## Purpose

Exposes interactive views for selected tool results through the MCP Apps extension, so a host that renders views shows an inventory a person can read and act on, while a host without the extension receives the same structured result unchanged.

## Requirements

### Requirement: Publish each app as a ui:// resource

The server SHALL publish every app in its inventory as a resource whose URI starts with `ui://`, whose MIME type is `text/html;profile=mcp-app`, and whose content is a single self-contained HTML document. App resources SHALL be listed whether or not a middleware session is available.

#### Scenario: Listing resources

- **WHEN** a client lists resources
- **THEN** every app's resource appears with the MCP Apps MIME type and a `_meta.ui` object

#### Scenario: Reading an app

- **WHEN** an app's resource URI is read
- **THEN** exactly one text content item is returned carrying the app's HTML document

### Requirement: Link each app's tool to its resource

A tool an app renders SHALL carry `_meta.ui.resourceUri` naming a published app resource, and SHALL be a read-only tool so a view may call it again to refresh without a consent prompt.

#### Scenario: Tool metadata

- **WHEN** a client lists tools with a session
- **THEN** each app's tool names its resource under `_meta.ui.resourceUri`
- **AND** the named resource is one the server publishes

### Requirement: Apps are inline

An app document SHALL reference no external script, stylesheet, image, frame, or network origin, so it renders under the default policy a host applies to an app that declares none.

#### Scenario: External reference

- **WHEN** an app document references an http, https, or protocol-relative URL in a src or href attribute
- **THEN** the app is refused by validation rather than registered

### Requirement: Apps speak the host lifecycle

An app SHALL send `ui/initialize` with the protocol version and then `ui/notifications/initialized`, render `ui/notifications/tool-result`, reflect `ui/notifications/tool-input` and `ui/notifications/tool-cancelled`, follow the host theme from the initialize result and `ui/notifications/host-context-changed`, report its size after rendering, and answer `ui/resource-teardown`.

#### Scenario: Result without structured content

- **WHEN** a host delivers a tool result carrying only text content
- **THEN** the app parses the text as the tool's structured result and renders it

#### Scenario: Refresh

- **WHEN** a person activates the refresh control
- **THEN** the app requests `tools/call` for its own tool through the host and renders the answer
- **AND** a refused call leaves the previous result on screen with the refusal shown

### Requirement: Inventory reports sections independently

The inventory tool SHALL fetch host identity, pools, datasets, apps, virtual machines, containers, SMB and NFS shares, and alerts independently, projecting each to summary fields, bounding datasets, and naming any section that could not be read under `errors` keyed by section. It SHALL fail as a whole only when no section could be read.

#### Scenario: Partial privilege

- **WHEN** the caller's key may read pools but is refused for apps
- **THEN** pools are returned, apps is an empty list, and `errors.apps` states the key is not permitted

#### Scenario: Nothing readable

- **WHEN** every section is refused
- **THEN** the tool returns an error rather than an empty inventory
