## Purpose

Provides the default read surface as a small set of concern-level action-dispatch tools, so that common questions about storage, sharing, apps, jobs, and system health are answerable in a single round trip without exposing any mutating operation.

## Requirements

### Requirement: Expose reads as concern-level dispatch tools

The server SHALL expose read operations through tools scoped to a user-facing concern rather than one tool per middleware method. Each tool SHALL take an `op` parameter selecting the operation.

The v1 concerns are `storage`, `sharing`, `apps`, `system`, and `jobs`.

#### Scenario: Default tool surface stays small

- **WHEN** a client lists the server's tools with the write tier disabled
- **THEN** the number of exposed tools does not exceed ten

#### Scenario: Reading pool capacity

- **WHEN** the model calls `storage` with the operation that lists pools
- **THEN** the server returns pool identity and capacity in a single tool call
- **AND** no additional tool call is required to answer how full a pool is

### Requirement: Constrain operations with a strict enum

Each dispatch tool's `op` parameter SHALL be a strict enumeration of that tool's supported operations.

#### Scenario: Unrecognized operation

- **WHEN** a tool is called with an `op` value outside its enumeration
- **THEN** the server returns a structured error
- **AND** the error lists the valid operations for that tool

### Requirement: Type arguments as a flat optional superset

Dispatch tool arguments SHALL be a flat set of optional parameters shared across that tool's operations, rather than a freeform object or a schema discriminated on `op`.

#### Scenario: Required argument omitted

- **WHEN** an operation that requires an identifier is called without one
- **THEN** the server returns a structured error naming the missing argument
- **AND** the error names the operation it applies to

#### Scenario: Irrelevant argument supplied

- **WHEN** an argument that does not apply to the selected operation is supplied
- **THEN** the server returns a structured error naming the arguments valid for that operation
- **AND** the server does not silently ignore the argument

#### Scenario: Arguments are not freeform

- **WHEN** a client inspects a dispatch tool's schema
- **THEN** every accepted argument is individually declared and typed
- **AND** no argument accepts an unconstrained object

### Requirement: Read tools contain no mutating operation

Every operation reachable through a read tool SHALL be free of side effects on the target. Read tools SHALL be annotated `readOnlyHint: true`.

#### Scenario: Annotation matches behavior

- **WHEN** the tool list is inspected
- **THEN** every concern-level dispatch tool is annotated read-only

#### Scenario: A mutating operation is introduced by mistake

- **WHEN** an operation whose underlying method mutates target state is added to a read tool's dispatch table
- **THEN** the build fails
- **AND** the failure names the offending operation and tool

### Requirement: Report per-operation authorization failures distinctly

When the configured API key lacks privilege for a requested operation, the server SHALL distinguish that from a failure of the operation itself.

#### Scenario: API key lacks privilege

- **WHEN** an operation is rejected by the target for insufficient privilege
- **THEN** the server returns a structured error identifying it as an authorization failure
- **AND** the error names the operation that was refused

### Requirement: Shape results for consumption rather than passing through raw payloads

Read operations SHALL return results shaped for the question being asked rather than the target's unmodified response, and SHALL bound result size.

#### Scenario: Large collection returned

- **WHEN** an operation would return more items than the configured result limit
- **THEN** the server returns the bounded set
- **AND** the response states the total count and that the result was truncated

#### Scenario: Verbose upstream payload

- **WHEN** the target returns a payload containing fields irrelevant to the operation
- **THEN** the server returns the fields relevant to the operation
- **AND** the response indicates how to retrieve the complete record

### Requirement: Response-shaping arguments apply uniformly across a concern's operations

An argument that only bounds or reshapes what a response contains, rather than selecting or narrowing the middleware call, SHALL apply the same way to every operation in a concern. Such an argument SHALL NOT be declared per-operation, and SHALL NOT be refused by an operation that does not separately declare it.

This is distinct from an argument that selects or narrows the middleware call itself, such as an identifier or a filter: those remain specific to the operations that accept them, and supplying one to an operation that does not is still refused per the "Type arguments as a flat optional superset" requirement above.

#### Scenario: Result-bounding argument supplied to an operation that never declared it

- **WHEN** a caller supplies the result-bounding argument to an operation that does not list it among its own accepted arguments
- **THEN** the server applies the bound to the operation's result
- **AND** the server does not refuse the call for supplying it

#### Scenario: Result-bounding argument supplied to a single-object operation

- **WHEN** a caller supplies the result-bounding argument to an operation whose result is a single object rather than a collection
- **THEN** the server returns the object unchanged
- **AND** the server does not refuse the call for supplying it

### Requirement: Declare middleware filter mappings per operation

An operation SHALL declare which of its arguments the server passes to the target as query filters, naming for each argument the middleware field it filters on and the comparison operator to apply. The dispatcher SHALL NOT hold its own set of filterable argument names.

Filtering SHALL be performed by the target rather than by the server post-filtering a larger response, so that a bound applies to the matching set rather than to an arbitrary page of it.

An argument's role is a property of the operation that declares it, not of the argument's name: the same argument name MAY be a positional identifier for one operation and a query filter for another, and each operation SHALL behave according to its own declaration.

#### Scenario: Filter argument supplied to an operation that declares it

- **WHEN** an operation is called with an argument it declares as a filter
- **THEN** the server narrows the request at the target using the declared field and operator
- **AND** the response reflects only matching records

#### Scenario: Filter argument supplied to an operation that does not declare it

- **WHEN** an operation is called with a filter argument it does not declare
- **THEN** the server returns a structured error naming the arguments valid for that operation
- **AND** the server does not silently ignore the argument

#### Scenario: Filtering a list-valued field

- **WHEN** an operation filters on a middleware field whose value is a list rather than a scalar
- **THEN** the server applies the membership operator that operation declared
- **AND** records whose list contains the supplied value are returned

#### Scenario: One argument name serving two roles

- **WHEN** one operation declares an argument as a positional identifier and another declares the same argument name as a query filter
- **THEN** each operation passes it to the target in the form it declared
- **AND** neither operation's behavior is determined by how the other declared it

#### Scenario: Result bound applies to the filtered set

- **WHEN** a filter argument and a result bound are supplied together
- **THEN** the bound applies to the records matching the filter
- **AND** the reported total is the number of matching records rather than the size of the unfiltered collection

### Requirement: Expose applications available to install as a read-only concern

The server SHALL expose the target's application catalog through a concern distinct from the one covering installed applications, so that "what can be installed" and "what is installed" are separately addressable. The concern SHALL cover browsing the catalog, retrieving the vocabulary of categories a caller may filter by, and retrieving one catalog entry in full by name.

Every operation in this concern SHALL be read-only. Installing, upgrading, or otherwise mutating an application SHALL NOT be reachable through it.

#### Scenario: Browsing the catalog

- **WHEN** a caller browses the catalog without narrowing it
- **THEN** the server returns a bounded set of entries reduced to identity and version fields
- **AND** the response states the total number of entries, that the result was truncated, and that fields were dropped

#### Scenario: Catalog entry detail

- **WHEN** a caller requests one catalog entry by name
- **THEN** the server returns that entry's complete record
- **AND** no further call is required to read its description, versions, or categories

#### Scenario: Narrowing the catalog by category

- **WHEN** a caller browses the catalog narrowed to a category
- **THEN** only entries belonging to that category are returned

#### Scenario: Discovering what may be filtered on

- **WHEN** a caller requests the catalog's categories
- **THEN** the server returns the category values the catalog uses
- **AND** a caller can narrow a browse by one of them without guessing the vocabulary

#### Scenario: Catalog concern carries no mutating operation

- **WHEN** the catalog concern's operations are inspected
- **THEN** none of them installs, upgrades, or removes an application
- **AND** the tool is annotated read-only

### Requirement: Project browse operations over large upstream collections

An operation whose upstream collection is large enough that returning it whole would exhaust a caller's context SHALL reduce each entry to a declared subset of fields by default, and SHALL report that it did so. The complete records SHALL remain retrievable.

#### Scenario: Upstream collection carries large per-entry documents

- **WHEN** an operation's upstream records embed documentation, schemas, or version histories
- **THEN** the default response omits them
- **AND** the response indicates how to retrieve the complete records

#### Scenario: Complete records requested explicitly

- **WHEN** a caller requests the unreduced form of a projected operation
- **THEN** the server returns every field of each entry

