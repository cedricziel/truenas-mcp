## ADDED Requirements

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
