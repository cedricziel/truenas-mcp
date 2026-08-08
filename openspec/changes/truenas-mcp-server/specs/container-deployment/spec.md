## Purpose

Packages the server as a container installable as a TrueNAS custom app, with a configuration surface and startup behavior suited to an appliance where the operator has limited visibility into what the container is doing.

## ADDED Requirements

### Requirement: Publish a container image to a public registry

The server SHALL be distributed as a container image published to GitHub Container Registry, runnable without a build step by the person installing it.

#### Scenario: Running the published image

- **WHEN** the published image is pulled from the registry and run with valid configuration
- **THEN** the server starts and serves MCP over its configured transport

#### Scenario: Image is versioned

- **WHEN** the image is published
- **THEN** it carries an immutable version tag identifying the release, in addition to any moving tag

#### Scenario: Image is pullable without authentication

- **WHEN** a user pulls the published image without registry credentials
- **THEN** the pull succeeds

### Requirement: Build and publish through continuous integration

Image builds and publication SHALL be performed by GitHub Actions rather than from a developer machine, so that every published image is reproducible from a known commit.

#### Scenario: Change pushed to the default branch

- **WHEN** a commit lands on the default branch
- **THEN** the workflow builds the image and publishes it under a moving tag

#### Scenario: Release tagged

- **WHEN** a release tag is pushed
- **THEN** the workflow publishes an image carrying the corresponding immutable version tag

#### Scenario: Tests or lint fail

- **WHEN** the test or lint stage fails
- **THEN** no image is published for that commit

#### Scenario: Published image is traceable to source

- **WHEN** a published image is inspected
- **THEN** it records the commit it was built from

### Requirement: Verify the published image before the server is feature-complete

The delivery pipeline SHALL be exercised from the first increment. A published image SHALL have been pulled from the registry and run on the target before feature work proceeds.

#### Scenario: Walking skeleton published and run

- **WHEN** the first increment is complete
- **THEN** an image containing a server that starts, validates configuration, and reports health has been published to the registry
- **AND** it has been pulled and run on the target TrueNAS instance

### Requirement: Provide a tested TrueNAS custom-app Compose definition

The project SHALL provide a Docker Compose definition that installs the server through the TrueNAS *Install via YAML* custom-app flow. The definition SHALL carry a top-level `services` key, as required from TrueNAS 25.10 onward.

#### Scenario: Installing through the TrueNAS apps UI

- **WHEN** the provided Compose definition is pasted into the custom app YAML editor with the required configuration supplied
- **THEN** the app deploys and the server becomes reachable at the configured port

#### Scenario: Compose definition is verified rather than illustrative

- **WHEN** the Compose definition is published
- **THEN** it has been installed successfully on a TrueNAS instance through the custom-app flow

### Requirement: Configure entirely through environment variables

All configuration SHALL be supplied as environment variables. The container SHALL NOT require a mounted configuration file or a persistent dataset in order to run.

#### Scenario: Configured without persistent storage

- **WHEN** the container runs with configuration supplied only as environment variables and no volume mounted
- **THEN** the server starts and operates normally

#### Scenario: Restart without state loss

- **WHEN** the container is restarted
- **THEN** the server resumes normal operation without any persisted state

### Requirement: Do not require access to the host middleware socket

The container SHALL reach the target only over the network. Its deployment definition SHALL NOT mount the host's middleware socket or otherwise require privileged host access.

#### Scenario: Deployment definition privileges

- **WHEN** the provided Compose definition is inspected
- **THEN** it mounts no host socket
- **AND** it does not request privileged execution or host namespace access

#### Scenario: Deployed away from the target

- **WHEN** the container runs on a machine other than the target TrueNAS instance
- **THEN** it operates identically given a reachable target address

### Requirement: Fail fast and legibly on invalid configuration

The server SHALL validate configuration at startup and SHALL refuse to start on invalid configuration, reporting the specific problem in its logs.

#### Scenario: Required configuration missing

- **WHEN** the container starts without a required configuration value
- **THEN** it exits rather than starting in a degraded state
- **AND** its logs name the missing value

#### Scenario: Target address unusable

- **WHEN** the configured target address is malformed
- **THEN** the container exits and its logs name the offending value

#### Scenario: Startup summary

- **WHEN** the server starts successfully
- **THEN** its logs report the target address, the transport and whether TLS is active, and whether the write tier is enabled

### Requirement: Report health for the app platform

The container SHALL expose a health signal reflecting whether the server is able to serve requests, so the apps platform can report the app's state accurately.

#### Scenario: Server is serving

- **WHEN** the server is running and able to accept MCP sessions
- **THEN** the health signal reports healthy

#### Scenario: Server cannot reach the target

- **WHEN** the target is unreachable
- **THEN** the health signal reports unhealthy
- **AND** the reason is available in the container's logs

### Requirement: Run without elevated privileges

The container SHALL run as a non-root user and SHALL NOT require added capabilities.

#### Scenario: Non-root execution

- **WHEN** the container runs with its default user
- **THEN** the process does not run as root
- **AND** the server operates normally
