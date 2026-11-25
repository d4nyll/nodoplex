# Base Specification

Read the [mental model](00_mental-model.md) first. This is a base specification, so don't implement it directly. Write a specification that extends it for a specific technology stack (e.g. [HTTP, JSON, server-sent events, and storage](02_extended-specification.md)). Then build the implementation against that extended specification. Anything this document does not specify, constrain, or require is left to the implementation.

## Conventions

- "The namespace" means the namespace of the request
- A **field** is a named value that a schema table or an input table lists
- The **range** of a field is the set of values that the Comments column of its table allows (e.g. "at least 1 character"). For a field that a row of the Node schema gives, its range is the one that the Node schema gives
- A schema type written as a list of values (e.g. `created`, `updated`, `deleted`) is a string that is one of those values
- If a namespace has no change entry, its **latest change ID** is 0. Otherwise, its latest change ID is the highest value of `changeId` of its change entries
- **id order** is ascending order of the values of `id`, with each value read as a 128-bit unsigned integer
- The schemas use these **schema types**:

  | Schema type | Meaning                                                                      |
  | ----------- | ---------------------------------------------------------------------------- |
  | string      | a sequence of Unicode code points                                            |
  | integer     | a whole number from -2^63 to 2^63 - 1, both inclusive                        |
  | uuid        | a 128-bit value                                                              |
  | date        | a date in the Gregorian calendar                                             |
  | timestamp   | an instant in UTC                                                            |
  | object      | a structured value that a client gives, in the form that the extended specification defines |
  | boolean     | `true` or `false`                                                            |
  | any         | a present value, in the form that the extended specification defines         |
  | X[]         | a sequence of values of schema type X                                        |

- An input can give a field with no value
- Two values are the **same** if they have the same schema type, and:
  - for two values of schema type uuid: they hold the same 128-bit value
  - for two strings: they have the same sequence of Unicode code points
  - for two integers: they are equal
  - for two dates: they name the same date
  - for two timestamps: they name the same instant
  - for two booleans: they are both `true`, or both `false`
  - for two values of schema type object: the extended specification defines when they are the same
  - for two values of schema type any: the extended specification defines when they are the same
  - for two sequences: they have the same length, and the same values in the same order
- A string **contains** another string if the code points of the second string occur in the first, in the same order, with no gap

## Schemas

### Node

A node of any type is allowed to carry these fields:

| Field         | Schema type | Set by | Comments                                                                                                         |
| ------------- | ----------- | ------ | ---------------------------------------------------------------------------------------------------------------- |
| `id`          | uuid             | server | unique among all the nodes that the server held at any time, in every namespace. The server must not modify it |
| `type`        | `NodeType`       | client | set when the node is created. The server must not modify it                                                      |
| `changeId`    | integer          | server | the version of the node. It is the `changeId` of the latest change entry whose `nodeId` is the `id` of the node |
| `description` | string           | client | tracked                                                                                                          |
| `fields`      | ArbitraryField[] | client | untracked. The arbitrary fields of the node. Every node type can carry them here. Present in an output only if the node has at least one. In an output, sort by `name` ascending, by Unicode code point |

A `NodeType` is a string that is one of the values in the `type` column of the table below. A node is allowed to carry a field of the Other fields column only if the table gives it for the type of the node:

| `type`        | Parent types                  | Other fields                                |
| ------------- | ----------------------------- | ------------------------------------------- |
| `vision`      | none                          | `title` (required)                          |
| `goal`        | `vision`                      | `title` (required), `targetDate`            |
| `feature`     | `goal`                        | `title` (required), `priority`              |
| `requirement` | `feature`                     | `title` (required), `acceptanceCriteria`    |
| `task`        | `requirement`                 | `title` (required), `status` (required)     |
| `subtask`     | `task`                        | `title` (required), `status` (required)     |
| `output`      | each type other than `output` | `title`, `outputType` (required), `payload` |

| Field                | Schema type  | Comments                                        |
| -------------------- | ------------ | ----------------------------------------------- |
| `title`              | string       | tracked. At least 1 character                   |
| `targetDate`         | date         | tracked                                         |
| `priority`           | integer      | tracked. A lower value means a higher priority  |
| `acceptanceCriteria` | string       | tracked                                         |
| `status`             | `TaskStatus` | tracked. The progress of the work on the Task or SubTask |
| `outputType`         | string       | tracked. At least 1 character                   |
| `payload`            | object       | tracked                                         |

A `TaskStatus` is a string that is one of these values:

- `backlog`
- `in_progress`
- `done`
- `blocked`

A **defined field** is `description`, `fields`, or a field that the Other fields column names for the type of the node. `id`, `type`, and `changeId` are not defined fields. A defined field is tracked or untracked, as its schema comment says. The name `fields` is the same for every node type.

In an output, a node must carry each field that has a value, and no other field.

### Arbitrary fields

An **arbitrary field** is a field that the client names. A node of any type can carry arbitrary fields in `fields`.

| Field     | Schema type | Set by | Comments                                                                                                      |
| --------- | ----------- | ------ | ------------------------------------------------------------------------------------------------------------- |
| `name`    | string      | client | from 1 to 64 characters, both inclusive. Each character is an ASCII letter, an ASCII digit, or `_`            |
| `value`   | any         | client |                                                                                                               |
| `tracked` | boolean     | client | if `true`, a change to `value` can be a recorded field change                                                 |

AF1. A node must not hold two arbitrary fields with the same `name`.

In an output, an arbitrary field must carry `name`, `value`, and `tracked`.

### Edge

| Field        | Schema type | Set by | Comments                                                                                     |
| ------------ | ----------- | ------ | -------------------------------------------------------------------------------------------- |
| `type`       | `EdgeType`  | client |                                                                                              |
| `tailId`     | uuid        | client | the `id` of the child of a hierarchical edge, or of the dependent of a dependency edge       |
| `headId`     | uuid        | client | the `id` of the parent of a hierarchical edge, or of the dependee of a dependency edge       |
| `tailType` | `NodeType`  | server | the type of the node whose `id` is the value of `tailId`                                   |
| `headType` | `NodeType`  | server | the type of the node whose `id` is the value of `headId`                                   |

An `EdgeType` is a string that is `hierarchical` or `dependency`. The **tail** of an edge is the node whose `id` is the value of `tailId` of the edge. The **head** of an edge is the node whose `id` is the value of `headId` of the edge. A hierarchical edge points from the child to the parent. A dependency edge points from the dependent to the dependee.

In an output, an edge must carry every field of the table above.

### ChangeEntry

| Field       | Schema type                     | Comments                                                                                    |
| ----------- | ------------------------------- | ------------------------------------------------------------------------------------------- |
| `changeId`  | integer                         | the ID of the change                                                                        |
| `action`    | `created`, `updated`, `deleted` |                                                                                             |
| `entity`    | `node`, `edge`                  | the kind of the item that changed                                                           |
| `nodeId`    | uuid                            | the `id` of the node. Present only if the value of `entity` is `node`                       |
| `nodeType`  | `NodeType`                      | the type of the node. Present only if the value of `entity` is `node`                       |
| `node`      | Node                            | the node after the change. Present only if the value of `entity` is `node`, and the value of `action` is not `deleted` |
| `previous`  | Node                            | the node before the change. Present only if the value of `entity` is `node`, and the value of `action` is not `created` |
| `edge`      | Edge                            | the edge. Present only if the value of `entity` is `edge`                                   |
| `timestamp` | timestamp                       | the time of the server's clock when the change took effect                                  |

In an output, a change entry must carry each field that has a value, and no other field.

A change entry **matches** a set of node types if one of these holds:

- the value of `entity` is `node`, and the value of `nodeType` is in the set
- the value of `entity` is `edge`, and the value of `tailType` or of `headType` of the edge is in the set

### Outputs

#### NodeList

| Field   | Schema type | Comments                                                 |
| ------- | ----------- | -------------------------------------------------------- |
| `nodes` | Node[]      |                                                          |
| `total` | integer     | the number of nodes that match the filters, on all pages |

#### NodeArray

| Field   | Schema type |
| ------- | ----------- |
| `nodes` | Node[]      |

#### Subgraph

| Field   | Schema type |
| ------- | ----------- |
| `nodes` | Node[]      |
| `edges` | Edge[]      |

#### ChangeList

| Field            | Schema type   | Comments                                                                    |
| ---------------- | ------------- | --------------------------------------------------------------------------- |
| `changes`        | ChangeEntry[] |                                                                             |
| `latestChangeId` | integer       | the latest change ID of the namespace, from the same state as `changes`    |

#### Namespace

| Field            | Schema type | Comments                                      |
| ---------------- | ----------- | --------------------------------------------- |
| `latestChangeId` | integer     | the latest change ID of the namespace         |
| `namespaceExposeChangefeed` | `enabled`, `disabled` | the namespace setting. `enabled` if the client has not set it |
| `exposeChangefeed`           | `enabled`, `disabled` | `enabled` only when both the server setting and the namespace setting are `enabled` |

#### Error

| Field     | Schema type | Comments                                          |
| --------- | ----------- | ------------------------------------------------- |
| `code`    | string      | the identifier of the refusal (Appendix A, or the extended specification) |
| `message` | string      | a description of the refusal for a person to read |

## Identity

Authentication and authorization are outside the scope of this specification.

## Behaviors and Rules

### Server rules

#### Namespaces

N1. The extended specification defines which strings are valid namespaces.

N2. The server must treat two namespaces as the same if and only if they match case-sensitively.

N3. An operation must read and write only the nodes, the edges, and the change entries in the namespace.

#### Nodes

T1. The server must return each value of `payload` as the same value as the `payload` in the input that set it.

T2. The server must return each value of schema type `any` as the same value as the input that set it.

#### Edges

G1. A namespace must not hold two edges with the same type, the same tail, and the same head.

P1. The head of a hierarchical edge must have one of the parent types that the Node schema gives for the type of its tail.

P2. Each node whose type is not `vision` must be the tail of at least one hierarchical edge.

D1. The tail and the head of a dependency edge must have the same type.

D2. The dependency edges in a namespace must not form a cycle.

#### Changes

C1. In a namespace, the value of `changeId` of the first change entry must be 1.

C2. In a namespace, the value of `changeId` must increase by 1 from each change entry to the next.

C3. After the server appends a change entry, the server must not modify that entry.

C4. The server must not remove a change entry.

#### Expose Changefeed

K1. Expose Changefeed has a server setting and a namespace setting. Each setting is `enabled` or `disabled`. Server configuration sets the server setting. A client must not set the server setting. A client sets the namespace setting with `SetExposeChangefeed`. If server configuration does not set the server setting, the server setting is `enabled`. If the client has not set the namespace setting, the namespace setting is `enabled`.

K2. Expose Changefeed is enabled for a namespace only when both its server setting and its namespace setting are `enabled`. Otherwise Expose Changefeed is disabled for that namespace.

K3. The server must append a change entry, and must update change IDs, whether Expose Changefeed is enabled or disabled.

K4. If Expose Changefeed is disabled for the namespace, `ListChanges` and `Subscribe` must refuse with `changefeed_disabled`. A client can still read nodes and edges.

#### Atomicity and order

These are the **read operations**:

- `GetNode`
- `ListNodes`
- `GetEdge`
- `GetParents`
- `GetChildren`
- `GetAncestors`
- `GetDescendants`
- `GetDependencies`
- `GetDependents`
- `GetNamespace`
- `ListChanges`
- steps 1 to 3 of `Subscribe`
- each run of steps 6 and 7 of `Subscribe`

S1. In a namespace, the server must run each call to `CreateNode`, `UpdateNode`, `DeleteNode`, `CreateEdge`, `DeleteEdge`, or `SetExposeChangefeed` one at a time.

S2. The changes that an operation makes and the change entries that it appends must be atomic.

S3. After the server tells a client that an operation succeeded, the changes that the operation made must remain after a restart of the server.

S4. If an operation succeeds before a read operation starts, the read operation must read a state that includes the changes of that operation.

S5. A read operation must compute its whole output from one state of the namespace.

#### Errors

E1. If the server cannot complete an operation for a reason that has no other `code`, the server must refuse with `internal_error`.

E2. The `message` of an `internal_error` must not contain any of these:

   - the text of the error that caused the `internal_error`
   - a host name that the server uses
   - a database name that the server uses
   - a user name that the server uses

### Client recommendations

A1. For each namespace, a client should store the highest change ID that it has processed. The client can then ask `ListChanges` or `Subscribe` only for change entries after that change ID.

## Operations

Each operation is a sequence of steps. Each operation takes the namespace as an input, besides the fields of its input table.

The server must check each request in this order before step 1, and must stop at the first check that refuses:

1. If the namespace is not valid as the extended specification defines, refuse with `invalid_request`
2. If a field of the input has a value outside its schema type or its range, refuse with `invalid_request`. This check also applies to each field of each object in a list field of the input. A field with no value passes this check
3. If the input omits a field that the input table marks required, or gives it no value, refuse with `invalid_request`

Then the server must run the steps of the operation in order, and must stop at the first refusal. The `code` of a refusal must be the code that the check, the rule, or the step names. If a request reaches a step that returns, the request succeeds.

These phrases recur in the steps below.

The **parents** of a node are the heads of the hierarchical edges whose tail is the node.

The **children** of a node are the tails of the hierarchical edges whose head is the node.

The **ancestors** of a node are its parents and the ancestors of its parents.

The **descendants** of a node are its children and the descendants of its children.

The **dependees** of a node are the heads of the dependency edges whose tail is the node.

The **dependents** of a node are the tails of the dependency edges whose head is the node.

An update makes a **recorded field change** if the node before the update and the node after the update differ in one of these ways:

- a tracked defined field gained a value, lost a value, or has a value that is not the same
- an arbitrary field is present before the update or after it, its `value` is not the same, and its `tracked` is `true` before the update or after it

A missing arbitrary field is not the same as an arbitrary field that is present. A missing `tracked` is not `true`.

To **record a change** with an action for a node or an edge is to run these steps:

1. Take the latest change ID of the namespace, plus 1, as the new change ID
2. For a change to a node with the action `created` or `updated`:
   1. Set the `changeId` of the node to the new change ID
3. Append a change entry with:
   - the new change ID as `changeId`
   - the action as `action`
   - `node` as `entity` for a change to a node, or `edge` for a change to an edge
   - for a change to a node, the `id` of the node as `nodeId`, and its type as `nodeType`
   - for a change to a node, if the action is not `deleted`, the node as `node`
   - for a change to a node, if the action is not `created`, the node before the change as `previous`
   - for a change to an edge, the edge as `edge`
   - the time of the server's clock as `timestamp`

### Nodes

`CreateNode`, `GetNode`, `ListNodes`, `UpdateNode`, `DeleteNode`.

#### `CreateNode`

Input:

| Field                                                                                    | Schema type              | Required                 | Comments                                                                                     |
| ---------------------------------------------------------------------------------------- | ------------------------ | ------------------------ | -------------------------------------------------------------------------------------------- |
| `type`                                                                                   | `NodeType`               | yes                      |                                                                                              |
| `parentIds`                                                                              | uuid[]                   | yes, except for `vision` | the `id` of each parent, in any order. The same `id` is allowed more than one time. If absent, `[]` |
| `description`                                                                            | string                   | no                       |                                                                                              |
| `fields`                                                                                 | ArbitraryField[]         | no                       |                                                                                              |
| each field that the Other fields column of the Node schema gives for the value of `type` | as the Node schema gives | as the Node schema gives |                                                                                              |

Output: the new `Node`.

Steps:

1. If the input table does not name a field of the input for the value of `type`, refuse with `invalid_request`
2. If a field of the input has no value, refuse with `invalid_request`
3. If the input has `fields`:
   1. If two arbitrary fields in `fields` have the same `name`, refuse with `invalid_request`
   2. For each arbitrary field in `fields`:
      1. If `value` or `tracked` has no value, refuse with `invalid_request`
4. If the value of `type` is not `vision`, and the value of `parentIds` is empty, refuse with `invalid_request`
5. For each `id` in the value of `parentIds`:
   1. If no node in the namespace has that `id`, refuse with `invalid_reference`
   2. If the type of that node is not one of the parent types of the value of `type`, refuse with `invalid_reference`
6. Atomically:
   1. Create a node in the namespace with a new `id`, and with each field of the input other than `parentIds`
   2. Record a change with the action `created` for the node
   3. For each distinct `id` in the value of `parentIds`, in id order:
      1. Create a hierarchical edge whose tail is the node, and whose head is the node with that `id`
      2. Record a change with the action `created` for the edge
7. Return the node

#### `GetNode`

Input:

| Field | Schema type | Required |
| ----- | ----------- | -------- |
| `id`  | uuid        | yes      |

Output: the `Node`.

Steps:

1. If no node in the namespace has the `id`, refuse with `not_found`
2. Return the node

#### `ListNodes`

Input:

| Field    | Schema type  | Required | Comments                                     |
| -------- | ------------ | -------- | -------------------------------------------- |
| `type`   | `NodeType`[] | no       |                                              |
| `status` | `TaskStatus` | no       |                                              |
| `search` | string       | no       |                                              |
| `offset` | integer      | no       | 0 or more. If absent, 0                      |
| `limit`  | integer      | no       | from 1 to 200, both inclusive. If absent, 50 |

Output: a `NodeList`.

Steps:

1. Select every node in the namespace
2. If the input has `type`, keep only the nodes whose type is in the value of `type`
3. If the value of `search` has at least 1 character:
   1. Compute the Unicode simple case folding of the value of `search`, and of each value of `title` and `description`
   2. Keep only the nodes in which the folded value of `title` or of `description` contains the folded value of `search`
4. If the input has `status`, keep only the Tasks and SubTasks whose value of `status` is the value in the input
5. Sort the selected nodes in id order
6. Skip as many nodes as the value of `offset`, from the start of that order. After them, keep at most as many nodes as the value of `limit`
7. Return a `NodeList`. Its field `nodes` holds the nodes that step 6 kept. Its field `total` holds the number of nodes that step 5 sorted

#### `UpdateNode`

Input:

| Field                                                                                     | Schema type                                            | Required | Comments                                                  |
| ----------------------------------------------------------------------------------------- | ------------------------------------------------------ | -------- | --------------------------------------------------------- |
| `id`                                                                                      | uuid                                                   | yes      |                                                           |
| `changeId`                                                                                | integer                                                | yes      | the `changeId` of the version that the client last read |
| `description`                                                                             | string, nullable                                       | no       |                                                           |
| `fields`                                                                                  | ArbitraryField[]                                       | no       | each entry writes one arbitrary field. An entry whose `value` has no value removes that field |
| each field that the Other fields column of the Node schema gives for the type of the node | as the Node schema gives, and nullable if not required | no       |                                                           |

Output: the `Node`.

Steps:

1. If no node in the namespace has the `id`, refuse with `not_found`
2. If the input table does not name a field of the input for this node, refuse with `invalid_request`
3. If the input gives no value to a field that the input table does not mark nullable, refuse with `invalid_request`
4. If the input has `fields`:
   1. If two arbitrary fields in `fields` have the same `name`, refuse with `invalid_request`
   2. For each arbitrary field in `fields`, if `value` has a value and `tracked` has no value, refuse with `invalid_request`
5. If the value of `changeId` is not the value of `changeId` of the node, refuse with `change_id_mismatch`
6. Atomically:
   1. Copy each value of the input to the node, except the values of `id`, `changeId`, and `fields`
   2. Remove each defined field that the input gives with no value. Leave every other defined field as it was
   3. If the input has `fields`, then for each arbitrary field in `fields`:
      1. If `value` has no value, remove the arbitrary field with that `name` from the node. If the node has no such arbitrary field, do nothing
      2. If `value` has a value, store an arbitrary field with that `name`, that `value`, and that `tracked`. Replace any arbitrary field that already has that `name`
   4. Compare the node with its state before step 6.1. If that comparison shows a recorded field change, record a change with the action `updated` for the node
7. Return the node

#### `DeleteNode`

Input:

| Field | Schema type | Required |
| ----- | ----------- | -------- |
| `id`  | uuid        | yes      |

Output: none.

Steps:

1. If no node in the namespace has the `id`, refuse with `not_found`
2. If the node has a child, refuse with `has_children`
3. Atomically:
   1. Take each edge whose tail or head is the node. Sort those edges in id order of `tailId`, then of `headId`, then by `type` with `dependency` before `hierarchical`
   2. Remove each of those edges in that order. For each edge, record a change with the action `deleted`
   3. Remove the node. Record a change with the action `deleted`
4. Return

### Edges

`CreateEdge`, `GetEdge`, `DeleteEdge`.

#### `CreateEdge`

Input:

| Field      | Schema type | Required |
| ---------- | ----------- | -------- |
| `type`     | `EdgeType`  | yes      |
| `tailId` | uuid        | yes      |
| `headId` | uuid        | yes      |

Output: the `Edge`.

Steps:

1. If no node in the namespace has the value of `tailId`, refuse with `invalid_reference`
2. If no node in the namespace has the value of `headId`, refuse with `invalid_reference`
3. If the value of `type` is `hierarchical`, and P1 forbids the type of the head, refuse with `invalid_reference`
4. If the value of `type` is `dependency`, and the tail and the head have different types, refuse with `invalid_reference`
5. If the namespace holds an edge with the same type, tail, and head, return that edge
6. If the value of `type` is `dependency`, and the new edge would break D2, refuse with `dependency_cycle`
7. Atomically:
   1. Create the edge
   2. Record a change with the action `created` for the edge
8. Return the edge

#### `GetEdge`

Input:

| Field      | Schema type | Required |
| ---------- | ----------- | -------- |
| `type`     | `EdgeType`  | yes      |
| `tailId` | uuid        | yes      |
| `headId` | uuid        | yes      |

Output: the `Edge`.

Steps:

1. If the namespace holds no edge with that type, tail, and head, refuse with `not_found`
2. Return the edge

#### `DeleteEdge`

Input:

| Field      | Schema type | Required |
| ---------- | ----------- | -------- |
| `type`     | `EdgeType`  | yes      |
| `tailId` | uuid        | yes      |
| `headId` | uuid        | yes      |

Output: none.

Steps:

1. If the namespace holds no edge with that type, tail, and head, refuse with `not_found`
2. If the value of `type` is `hierarchical`, and the tail has no other parent, refuse with `parent_required`
3. Atomically:
   1. Remove the edge
   2. Record a change with the action `deleted` for the edge
4. Return

### Traversals

`GetParents`, `GetChildren`, `GetAncestors`, `GetDescendants`, `GetDependencies`, `GetDependents`. Each of them takes this input:

| Field | Schema type | Required |
| ----- | ----------- | -------- |
| `id`  | uuid        | yes      |

#### `GetParents`

Output: a `NodeArray`.

Steps:

1. If no node in the namespace has the `id`, refuse with `not_found`
2. Return a `NodeArray` of the parents of the node, in id order

#### `GetChildren`

Output: a `NodeArray`.

Steps:

1. If no node in the namespace has the `id`, refuse with `not_found`
2. Return a `NodeArray` of the children of the node, in id order

#### `GetAncestors`

Output: a `Subgraph`.

Steps:

1. If no node in the namespace has the `id`, refuse with `not_found`
2. Return a `Subgraph` with these fields:
   - `nodes`: each ancestor of the node one time, in id order
   - `edges`: each hierarchical edge whose tail is the node or an ancestor, and whose head is an ancestor. The edges are in id order of the values of `tailId`, and then of `headId`

#### `GetDescendants`

Output: a `Subgraph`.

Steps:

1. If no node in the namespace has the `id`, refuse with `not_found`
2. Return a `Subgraph` with these fields:
   - `nodes`: each descendant of the node one time, in id order
   - `edges`: each hierarchical edge whose tail is a descendant, and whose head is the node or a descendant. The edges are in id order of the values of `tailId`, and then of `headId`

#### `GetDependencies`

Output: a `NodeArray`.

Steps:

1. If no node in the namespace has the `id`, refuse with `not_found`
2. Return a `NodeArray` of the dependees of the node, in id order

#### `GetDependents`

Output: a `NodeArray`.

Steps:

1. If no node in the namespace has the `id`, refuse with `not_found`
2. Return a `NodeArray` of the dependents of the node, in id order

### Changefeed

`ListChanges`, `Subscribe`.

#### `ListChanges`

Input:

| Field   | Schema type  | Required | Comments                                       |
| ------- | ------------ | -------- | ---------------------------------------------- |
| `since` | integer      | no       | 0 or more, exclusive. If absent, 0             |
| `limit` | integer      | no       | from 1 to 1000, both inclusive. If absent, 100 |
| `type`  | `NodeType`[] | no       |                                                |

Output: a `ChangeList`.

Steps:

1. If Expose Changefeed is disabled for the namespace, refuse with `changefeed_disabled`
2. If the value of `since` is above the latest change ID of the namespace, refuse with `change_out_of_range`
3. Select every change entry in the namespace whose value of `changeId` is above the value of `since`
4. If the input has `type`, keep only the change entries that match the value of `type`
5. Sort the selected change entries in ascending order of `changeId`. Keep at most as many change entries as the value of `limit`, from the start of that order
6. Return a `ChangeList`. Its field `changes` holds the change entries that step 5 kept. Its field `latestChangeId` holds the latest change ID of the namespace

#### `Subscribe`

Input:

| Field   | Schema type  | Required | Comments  |
| ------- | ------------ | -------- | --------- |
| `since` | integer      | no       | 0 or more, exclusive. If absent, the latest change ID |
| `type`  | `NodeType`[] | no       |           |

Output: a stream of messages. Each message holds one change entry. The `changeId` of each change entry is greater than the start change ID. If no change entry has a greater `changeId`, the stream holds no change entry until one is appended.

Steps:

1. If Expose Changefeed is disabled for the namespace, refuse with `changefeed_disabled`
2. Set the start change ID:
   - if the input has `since`, to its value
   - otherwise, to the latest change ID of the namespace
3. If the start change ID is above the latest change ID of the namespace, refuse with `change_out_of_range`
4. Open the stream
5. Set the **position** to the start change ID
6. If Expose Changefeed is disabled for the namespace, end the stream and stop. Otherwise select every change entry in the namespace whose value of `changeId` is above the position. If the server waits for a later change entry before this select, the wait must end when Expose Changefeed becomes disabled for the namespace
7. If the input has `type`, keep only the change entries that match the value of `type`
8. Send a message for each selected change entry, one time each, in ascending order of `changeId`
9. If step 6 selected change entries, set the position to the highest value of `changeId` among them
10. If Expose Changefeed is disabled for the namespace, end the stream and stop
11. Do steps 6 to 11 again, until the stream closes

### Namespace

`GetNamespace`, `SetExposeChangefeed`.

#### `GetNamespace`

Output: a `Namespace`.

Steps:

1. Return a `Namespace`. Its field `latestChangeId` is the latest change ID of the namespace. Its field `namespaceExposeChangefeed` is the namespace setting. Its field `exposeChangefeed` is `enabled` or `disabled`, as K2 gives for the namespace

#### `SetExposeChangefeed`

Input:

| Field                        | Schema type           | Required | Comments                                      |
| ---------------------------- | --------------------- | -------- | --------------------------------------------- |
| `namespaceExposeChangefeed` | `enabled`, `disabled` | yes      | the namespace setting. This operation must not change the server setting |

Output: a `Namespace`.

Steps:

1. Store the value of `namespaceExposeChangefeed` as the namespace setting
2. Return a `Namespace`, as step 1 of `GetNamespace` does

## Decisions left to the extended specification

The extended specification must define each of these:

- the transport, and how a request names the operation, the namespace, and each field of the input
- how each schema type is written
- when two values of schema type object are the same
- when two values of schema type any are the same
- how an input gives a field with no value
- how a refusal returns its `code` and its `message`
- how a client opens a `Subscribe` stream, how the server sends each message, and how either side closes the stream
- which strings are valid namespaces
- the code of the refusal of an input above a size limit of the implementation

## Appendix A: refusal codes

The table below gives each refusal that a check before step 1, a rule, or an operation step names.

| `code`                | Raised by                                     | Condition                                                                                                     |
| --------------------- | --------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| `invalid_request`     | every operation                               | the request fails one of the checks before step 1, or a step that names this code                             |
| `not_found`           | each operation with the input `id`, `GetEdge`, and `DeleteEdge` | no node in the namespace has the `id`, or the namespace holds no edge with the type, tail, and head of the input |
| `invalid_reference`   | `CreateNode`, `CreateEdge`                    | an `id` in the input is the `id` of no node in the namespace, or of a node whose type P1 or D1 forbids        |
| `dependency_cycle`    | `CreateEdge`                                  | the new edge would break D2                                                                                   |
| `parent_required`     | `DeleteEdge`                                  | the hierarchical edge is the only hierarchical edge whose tail is the tail of that edge                      |
| `has_children`        | `DeleteNode`                                  | the node has a child                                                                                          |
| `change_id_mismatch`  | `UpdateNode`                                  | the value of `changeId` in the input is not the value of `changeId` of the node |
| `change_out_of_range` | `ListChanges`, `Subscribe`                    | the value of `since` (`ListChanges`) or the start change ID (`Subscribe`) is above the latest change ID of the namespace |
| `changefeed_disabled` | `ListChanges`, `Subscribe`                    | Expose Changefeed is disabled for the namespace                                                               |
| `internal_error`      | every operation                               | the server cannot complete the operation for a reason that has no other `code` in this table                 |
