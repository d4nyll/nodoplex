# Base Specification

Read the [mental model](00_mental-model.md) first. This document is a base specification, so don't implement it directly. Write a specification that extends it for a specific technology stack (e.g. [HTTP, JSON, server-sent events, and storage](02_extended-specification.md)). Then build the implementation against that extended specification. Anything that this document doesn't specify, constrain, or require is left to the implementation.

## Conventions

- "Must", "must not", and "may" give the requirements and the permissions of the server, unless the sentence names another subject (e.g. a client, the extended specification)
- A **member** is a named value that a schema table or an input table lists. A **Field** is a Field of a Node, as [Fields](#fields) defines it
- "The slug" means the slug that the request names. "The Product" means the Product that has that slug
- The **range** of a member or a Field is the set of values that the Comments column of its table allows (e.g. "at least 1 character")
- A schema type written as a list of values (e.g. `creation`, `update`, `deletion`) is a string that is one of those values
- In the Required column of an input table, "yes" means that the input must give the member
- In **id order**, the values of `id` are in ascending order, with each value read as a 128-bit unsigned integer
- In **code point order**, strings are in ascending lexicographic order, with each character compared by its Unicode code point
- The schemas use these schema types:

  | Schema type  | Meaning                                                                                      |
  | ------------ | -------------------------------------------------------------------------------------------- |
  | string       | a sequence of Unicode scalar values (i.e. code points other than U+D800 to U+DFFF)           |
  | integer      | a whole number from -2^63 to 2^63 - 1, both inclusive                                        |
  | uuid         | a 128-bit value                                                                              |
  | timestamp    | an instant in UTC                                                                            |
  | boolean      | `true` or `false`                                                                            |
  | object       | a structured value that a client gives, in the form that the extended specification defines |
  | X[]          | a sequence of values of schema type X                                                        |
  | X, nullable  | a value of schema type X, or no value                                                        |

- An input can give no value to a member only if the schema type of the member is nullable. An input can give no value to a Field only if the Field is optional
- Two values are the **same** if they have the same schema type, and:
  - for two values of schema type uuid: they hold the same 128-bit value
  - for two strings: they have the same sequence of Unicode code points
  - for two integers: they are equal
  - for two timestamps: they name the same instant
  - for two booleans: they are both `true`, or both `false`
  - for two values of schema type object: the extended specification defines when they are the same
  - for two sequences: they have the same length, and the same values in the same order
  - for two values of a schema that this document defines (e.g. `Output`): each member is absent from both, or has the same value in both

  No value is the same as no value, and isn't the same as any value.

## Schemas

### Node Types

A `NodeType` is a string that is one of the values in the `type` column of the table below. The **Parent Type** of a Node Type is the Type that each Parent of a Node of that Type must have (G2).

| `type`        | Parent Type   |
| ------------- | ------------- |
| `product`     | none          |
| `vision`      | `product`     |
| `goal`        | `vision`      |
| `feature`     | `goal`        |
| `requirement` | `feature`     |
| `task`        | `requirement` |
| `subtask`     | `task`        |

### Other named schema types

| Schema type | Meaning                                       |
| ----------- | --------------------------------------------- |
| `EdgeKind`  | `hierarchical`, `dependency`                  |
| `Status`    | `not_started`, `in_progress`, `done`          |
| `UserId`    | a user ID, which is a string of at least 1 character |
| `Slug`      | a string that is a valid slug (P1)            |

### Fields

A Node stores the details of the work and the metadata of the Node in Fields. The table below defines the Fields of this document. Each row defines a separate Field for each Type that the Types column names. Those Fields have the same name and the same properties. An extended specification can define more Fields (N5).

The columns of the table give these properties of each Field:

- Types: the Node Types that have the Field
- Schema type: the schema type of the value. Together with the range, it gives the valid values of the Field
- Required: "yes" if each Node of the Type must always hold a value for the Field. Otherwise, the Field is optional
- Tracked: "yes" if the Field is tracked, and "no" if the Field is untracked. A change to the value of a tracked Field changes the state of the Node (C3)
- Set by: who sets the value of the Field
- Comments: what the Field means, its range, and its value if the request that creates the Node doesn't give it

| Field         | Types      | Schema type | Required | Tracked | Set by                                                | Comments                                                                                                       |
| ------------- | ---------- | ----------- | -------- | ------- | ----------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `product`     | every Type | `Slug`      | yes      | yes     | server                                                | the slug of the Product whose graph holds the Node                                                             |
| `title`       | every Type | string      | yes      | yes     | client                                                | at least 1 character                                                                                           |
| `description` | every Type | string      | no       | yes     | client                                                |                                                                                                                |
| `outputs`     | every Type | `Output`[]  | yes      | yes     | client                                                | the Outputs of the Node. The list can be empty. If the request that creates the Node doesn't give it, `[]`    |
| `status`      | every Type | `Status`    | yes      | yes     | client while the Node is a leaf (T1). Otherwise, server (T4) | the progress of the work. If the request that creates the Node doesn't give it, `not_started`           |
| `assignee`    | every Type | `UserId`    | no       | yes     | client                                                | the user ID of the user who is assigned the work. If the Field has no value, the Node has no assignee         |
| `slug`        | `product`  | `Slug`      | yes      | yes     | server                                                | the slug of the Product. The server sets it to the slug that the `CreateProduct` request names                 |

This document defines no untracked Field.

A value of schema type `Fields` holds the values of a set of Fields of one Node, with at most one value for each Field name. In a Node, `fields` holds each Field of the Node that has a value, and no other Field.

### Output

An `Output` represents a result of the work that a Node represents (e.g. a Git branch, a Slack message).

| Member       | Schema type | Required | Comments                                                       |
| ------------ | ----------- | -------- | -------------------------------------------------------------- |
| `outputType` | string      | yes      | at least 1 character. The kind of result (e.g. `git_branch`)   |
| `title`      | string      | no       | at least 1 character                                           |
| `payload`    | object      | no       | the details of the result                                      |

### Node

| Member         | Schema type  | Comments                                                                                                  |
| -------------- | ------------ | --------------------------------------------------------------------------------------------------------- |
| `id`           | uuid         | the ID of the Node (N1)                                                                                   |
| `type`         | `NodeType`   | the Type of the Node                                                                                      |
| `version`      | integer      | the version of the Node (V1)                                                                              |
| `fields`       | `Fields`     | each Field of the Node that has a value                                                                   |
| `watchers`     | `UserId`[]   | the user ID of each watcher of the Node, one time each, in code point order. The list can be empty       |
| `archivedMark` | boolean      | `true` if the Node carries the archived mark (A1)                                                         |
| `archived`     | boolean      | `true` if the Node is archived (A2)                                                                       |

In an output, a Node must carry each member of the table above. A Node doesn't hold its Edges.

### Edge

| Member     | Schema type | Comments                                                                                          |
| ---------- | ----------- | ------------------------------------------------------------------------------------------------- |
| `id`       | uuid        | the ID of the Edge (G1)                                                                           |
| `kind`     | `EdgeKind`  | the kind of the Edge                                                                              |
| `tailId`   | uuid        | the `id` of the Tail Node, which is the Child of a Hierarchical Edge, or the Dependent of a Dependency Edge |
| `headId`   | uuid        | the `id` of the Head Node, which is the Parent of a Hierarchical Edge, or the Prerequisite of a Dependency Edge |
| `priority` | integer     | the priority of a Hierarchical Edge (G7). Absent from a Dependency Edge                          |

In an output, an Edge must carry each member that has a value, and no other member.

### ChangeEntry

| Member     | Schema type | Comments                                                    |
| ---------- | ----------- | ----------------------------------------------------------- |
| `changeId` | integer     | the Change ID of the Change Entry (C1)                      |
| `time`     | timestamp   | the time at which the write took effect (C7)                |
| `userId`   | `UserId`    | the user ID of the request that made the write              |
| `changes`  | `Change`[]  | the changes of the write (C5), in any order                 |

### Change

| Member   | Schema type                                     | Comments                                                                     |
| -------- | ----------------------------------------------- | ---------------------------------------------------------------------------- |
| `kind`   | `creation`, `update`, `deletion`                | the kind of the change                                                       |
| `entity` | `node`, `edge`                                  | `node` for a change to a Node, or `edge` for a change to an Edge             |
| `before` | `Node` or `Edge`, as the value of `entity` gives | the Node or the Edge before the write (C6). Absent if the value of `kind` is `creation` |
| `after`  | `Node` or `Edge`, as the value of `entity` gives | the Node or the Edge after the write (C6). Absent if the value of `kind` is `deletion`  |

In an output, a Change Entry and each Change in it must carry each member that has a value, and no other member.

### FilterGroup

A `FilterGroup` is one filter group of a poll or a subscription (Q1). Each member of a `FilterGroup` is one filter. A `FilterGroup` must have at least one member.

| Member     | Schema type                            | Required | Comments                                                                  |
| ---------- | -------------------------------------- | -------- | ------------------------------------------------------------------------- |
| `type`     | `NodeType`[]                           | no       | at least 1 value                                                          |
| `watcher`  | (`UserId`, nullable)[]                 | no       | at least 1 value. No value matches a Node that has no watchers            |
| `archived` | `StateFilter` of boolean               | no       |                                                                           |
| `status`   | `StateFilter` of `Status`              | no       |                                                                           |
| `assignee` | `StateFilter` of (`UserId`, nullable)  | no       | No value matches a Node that has no assignee                              |

### StateFilter

A `StateFilter` of X holds values of schema type X, and gives which Node the server checks (Q6).

| Member         | Schema type                          | Required | Comments                                                  |
| -------------- | ------------------------------------ | -------- | --------------------------------------------------------- |
| `check`        | `before`, `after`, `both`, `either`  | yes      | which Node the server checks                              |
| `values`       | X[]                                  | no       | at least 1 value. The values for each Node that the server checks |
| `beforeValues` | X[]                                  | no       | at least 1 value. The values for the Node before the change |
| `afterValues`  | X[]                                  | no       | at least 1 value. The values for the Node after the change  |

A `StateFilter` must give either `values` alone, or `beforeValues` and `afterValues` together. It can give `beforeValues` and `afterValues` only if the value of `check` is `both` or `either`.

### ParentRef

A `ParentRef` names one Parent of a new Node.

| Member     | Schema type | Required | Comments                                                       |
| ---------- | ----------- | -------- | -------------------------------------------------------------- |
| `id`       | uuid        | yes      | the `id` of the Parent                                         |
| `priority` | integer     | no       | the priority of the new Hierarchical Edge. If absent, G7 gives it |

### Outputs of operations

#### NodePage

| Member    | Schema type | Comments                                                                                  |
| --------- | ----------- | ----------------------------------------------------------------------------------------- |
| `nodes`   | `Node`[]    | the Nodes of the page                                                                     |
| `lastKey` | integer     | the sort key of the last Node in `nodes` (`ListNodes`). Absent if `nodes` is empty        |

#### ProductList

| Member     | Schema type | Comments            |
| ---------- | ----------- | ------------------- |
| `products` | `Node`[]    | the Product Nodes   |

#### EdgeList

| Member  | Schema type | Comments          |
| ------- | ----------- | ----------------- |
| `edges` | `Edge`[]    | in id order       |

#### NextWork

| Member | Schema type | Comments                                                          |
| ------ | ----------- | ----------------------------------------------------------------- |
| `node` | `Node`      | the next available leaf (W2). Absent if the Product has none      |

#### ChangePage

| Member           | Schema type     | Comments                                                                         |
| ---------------- | --------------- | -------------------------------------------------------------------------------- |
| `changeEntries`  | `ChangeEntry`[] | in ascending order of Change ID                                                  |
| `latestChangeId` | integer         | the latest Change ID of the Product, from the same state as `changeEntries` (S5) |

#### LatestChangeId

| Member           | Schema type | Comments                             |
| ---------------- | ----------- | ------------------------------------ |
| `latestChangeId` | integer     | the latest Change ID of the Product  |

#### Error

| Member    | Schema type | Comments                                                                   |
| --------- | ----------- | -------------------------------------------------------------------------- |
| `code`    | string      | the identifier of the refusal (Appendix A, or the extended specification)  |
| `message` | string      | a description of the refusal for a person to read                          |

## Rules

### Users

U1. Every request carries the user ID of the user who sends the request. The server must accept each valid user ID without a check of who sent the request. The server must not authenticate or authorize a user.

### Products

P1. The extended specification must define which strings are valid slugs.

P2. The server must treat 2 slugs as the same only if they are the same string. Apart from P1, the server must not interpret the characters of a slug.

P3. The server must not create a Product whose slug is the slug of a Product that exists, or of a deleted Product. The server must keep the slug of each deleted Product, and must never delete it.

P4. Each Product has exactly one Product Node, which is the root Node of the graph of the Product. A Node is **in the Product** if the value of its `product` Field is the slug. An Edge is in the Product if its Tail Node is in the Product.

P5. An operation that names a Product must read and write only the Nodes, the Edges, and the Changefeed of that Product.

P6. If a request names a Node or an Edge by its `id`, and the Node or the Edge isn't in the Product, the server must answer as if the Node or the Edge doesn't exist.

P7. When `DeleteNode` deletes a Product Node, the Product becomes a **deleted Product**. The server must keep the Changefeed of a deleted Product for 30 days of 24 h each, from the `time` of the Change Entry that holds the deletion. Until that time ends, the server must run `Poll`, `Subscribe`, and `GetLatestChangeId` for the deleted Product as for a Product that exists, and must answer each other request for the deleted Product as if the Product doesn't exist.

P8. When the time that P7 gives ends, the server must delete the Changefeed of the deleted Product, and must end each open subscription to that Changefeed with `not_found`. From then on, the server must answer each request for the deleted Product as if the Product doesn't exist.

### Nodes and Fields

N1. The server must give each new Node an `id` that is unique among all the Nodes that the server held at any time, in every Product.

N2. The server must not change the `id` or the `type` of a Node, or the value of its `product` Field or its `slug` Field.

N3. Each Node must hold a value for each required Field of its Type.

N4. The server must refuse an input that names a Field that neither this document nor the extended specification defines for the Type of the Node. A client can't define a Field.

N5. An extended specification may define more Fields. For each Field that it defines, it must give the properties that each column of the Fields table gives. It must not define a Field with the name of a Field that this document defines for the same Type. If the server sets the Field, the extended specification must define when the server sets it, and to which value.

N6. The server must return each value of schema type object as a value that is the same as the value that the input gave.

### Edges

These terms describe the graph of a Product:

- An Edge points from its Tail Node to its Head Node
- The **Parents** of a Node are the Head Nodes of the Hierarchical Edges that point from the Node. The **Children** of a Node are the Tail Nodes of the Hierarchical Edges that point to the Node
- The **ancestors** of a Node are its Parents and the ancestors of its Parents. The **descendants** of a Node are its Children and the descendants of its Children
- The **Prerequisites** of a Node are the Head Nodes of the Dependency Edges that point from the Node. The **Dependents** of a Node are the Tail Nodes of the Dependency Edges that point to the Node

G1. The server must give each new Edge an `id` that is unique among all the Edges that the server held at any time, in every Product.

G2. The Head Node of a Hierarchical Edge must have the Parent Type of the Type of its Tail Node.

G3. A Product must have no Parent. A Vision must have exactly one Parent, which is the Product Node of its graph. Each Node of another Type must have at least one Parent.

G4. The Tail Node and the Head Node of an Edge must have the same value of the `product` Field.

G5. The server must not hold 2 Edges that connect the same 2 Nodes, whatever their kinds and their directions.

G6. A Dependency Edge can connect 2 Nodes of any Types, with any Parents, unless Y1 or Y2 forbids it.

G7. Each Hierarchical Edge must hold a priority. A lower number means a higher priority. 2 Hierarchical Edges that point to the same Parent can have the same priority. If a request creates a Hierarchical Edge and gives no priority, the server must give the Edge a priority that is 1 greater than the largest priority of the other Hierarchical Edges that point to the same Parent, or 10 if no other Hierarchical Edge points to that Parent. The other Hierarchical Edges include each Edge whose Child is archived. If the largest priority is 2^63 - 1, the server must refuse the write with `priority_out_of_range`.

G8. The server must not check priorities against Dependency Edges.

G9. The server must not change the `id`, the `kind`, the `tailId`, or the `headId` of an Edge. The priority of a Hierarchical Edge is the only part of an Edge that can change.

### Cycles

Node B is **reachable** from Node A through some relations (e.g. Parents and Prerequisites) if B is A, or if B is reachable through those relations from a Node that those relations give for A.

Y1. The server must refuse with `cycle` a write that would form a cycle in which each Node is a Child or a Dependent of the next Node, and the last Node is a Child or a Dependent of the first Node. A new Edge would form this kind of cycle if its Tail Node is reachable from its Head Node through Parents and Prerequisites.

Y2. The server must refuse with `cycle` a write that would form a cycle in which each Node is a Parent or a Dependent of the next Node, and the last Node is a Parent or a Dependent of the first Node. A new Hierarchical Edge would form this kind of cycle if its Parent is reachable from its Child through Children and Prerequisites. A new Dependency Edge would form this kind of cycle if its Dependent is reachable from its Prerequisite through Children and Prerequisites.

Y3. The checks of Y1 and Y2 must count archived Nodes and their Edges as any other Nodes and Edges.

Before each write, the graph holds no cycle of either kind. Only a write that creates an Edge can form one, and the cycle includes the new Edge. So the checks in Y1 and Y2 find each cycle that a write would form. An implementation can use other steps that give the same result.

### Archiving

A1. Each Node carries the **archived mark** or doesn't carry it. `ArchiveNode` places the archived mark on a Node, and `UnarchiveNode` removes it. A client can archive a Node of any Type. A new Node doesn't carry the archived mark.

A2. A Node is **archived** if one of these is true:

   - The Node carries the archived mark
   - The Node has at least one Parent, and each path of Hierarchical Edges from the Node to the Product Node passes through a Node that carries the archived mark. Each path includes the Node and the Product Node

   Thus a Node is archived if it carries the archived mark, or if it has at least one Parent and each of its Parents is archived.

A3. After each write, the value of `archived` of each Node must be the value that A2 gives for the state after the write. The server must compute that value from the archived marks and the Hierarchical Edges.

A4. Unless a read asks to include archived Nodes, the server must leave out of the read each archived Node, and each Edge that has an archived Node at either end. Each read by ID asks with `includeArchived`, and `ListNodes` and `ListProducts` ask with the `archived` filter.

A5. The server must accept a write to an archived Node, as it accepts the same write to a Node that isn't archived.

### Status

T1. A **leaf** is a Node that has no Children. A Child counts whether it is archived or not. A client can set the status of a leaf. The server must refuse with `status_is_derived` a request that sets the status of a Node that isn't a leaf.

T2. A Node is **blocked** if at least one of its Prerequisites is neither `done` nor archived.

T3. The **derived status** of a Node that isn't a leaf is the result of these steps:

   1. Leave out each Child that carries the archived mark. Count each other Child, including a Child that is archived only because of the archived marks on its ancestors
   2. Check the Children that are left against these rules, in order, and use the first rule that applies:
      1. If no Children are left: if the Node isn't blocked, the derived status is `done`. Otherwise, it is `in_progress`
      2. If each Child is `done`, and the Node isn't blocked, the derived status is `done`
      3. If each Child is `not_started`, the derived status is `not_started`
      4. Otherwise, the derived status is `in_progress`

T4. After each write, the status of each Node that isn't a leaf must be its derived status in the state after the write. The derived status of a Node depends on the statuses of its Children and of its Prerequisites. Y2 forbids a cycle of these relations, so exactly one set of statuses meets T4.

T5. A leaf must keep its status until a client sets a new status. Thus a Node that stops having Children keeps its last derived status.

T6. The server must refuse with `done_leaf_blocked` a write after which a leaf whose status is `done` is blocked. This applies to each leaf in the Product, archived or not, and however many Edges away from the Nodes that the write changes.

### Available work

W1. A Node is **available** if each of these is true:

   - It is a leaf
   - Its status is `not_started`
   - It isn't archived
   - It has no assignee
   - It isn't blocked

W2. The **next available leaf** of a Product is the result of a visit to its Product Node. A visit to a Node runs these steps:

   1. If the Node is archived or blocked, return no Node
   2. If the Node is available, return the Node
   3. For each Child of the Node, in ascending order of the priority of the Hierarchical Edge from the Child to the Node, and then in id order: visit the Child. If the visit returns a Node, return that Node
   4. Return no Node

   An implementation can use other steps that give the same result.

### Versions

V1. The **version** of a Node is the Change ID of the newest Change Entry that holds the creation or an update of the Node.

V2. A request to each of these operations must give the version of a Node that the client last read:

   - `UpdateNode`, `ArchiveNode`, `UnarchiveNode`, and `DeleteNode`: the version of the Node of the request
   - `CreateEdge`, `UpdateEdge`, and `DeleteEdge`: the version of the Tail Node of the Edge

   An `UpdateNode` request that names only untracked Fields doesn't have to give a version.

V3. If a request gives a version, and the version of the Node isn't the version that the request gives, the server must refuse the write with `version_conflict`. If an `UpdateNode` request gives no version, the server must not make this check.

V4. `WatchNode` and `UnwatchNode` must not take a version, and must not change the version of a Node.

### The Changefeed

C1. Each Product has one Changefeed. In each Changefeed, the Change IDs must start at 1, and must increase by 1 with each Change Entry, in the order in which the writes take effect.

C2. The **latest Change ID** of a Changefeed is the highest Change ID that the server has given to a Change Entry in that Changefeed. The server must store the latest Change ID of each Changefeed. The server must not compute the latest Change ID, or the next Change ID, from the Change Entries that it still keeps. The server must never give the same Change ID to 2 Change Entries of the same Changefeed, even after it deletes Change Entries.

C3. The **state** of a Node is made of these values:

   - its tracked Fields
   - whether it carries the archived mark
   - whether it is archived
   - the Edges that point from it, with their priorities

   The state of an Edge is its priority.

C4. A write must record exactly one Change Entry if it creates or deletes a Node or an Edge, or changes the state of a Node or an Edge. Otherwise, the write must record no Change Entry. Thus a write that changes only untracked Fields records no Change Entry, and neither does `WatchNode` or `UnwatchNode`.

C5. The `changes` of a Change Entry must hold exactly these changes:

   - a creation for each Node and each Edge that the write creates
   - a deletion for each Node and each Edge that the write deletes
   - an update for each other Node and each other Edge whose state the write changes

   Thus `changes` holds an update of each Node whose derived status changes, of each Node that becomes archived or stops being archived, and of the Tail Node of each Edge that the write creates, updates, or deletes, unless the write creates or deletes that Node.

C6. The `before` of a change must hold the Node or the Edge as it was before the write. The `after` of a change must hold the Node or the Edge as it is after the write. The `version` of the Node in `after` is the Change ID of the Change Entry.

C7. The `time` of a Change Entry must be the later of these 2 times: the time of the server's clock when the server records the Change Entry, and the `time` of the previous Change Entry of the same Changefeed. This applies also if the server has deleted the previous Change Entry. Thus the `time` of the Change Entries of a Changefeed never decreases, even if the server's clock is set back.

C8. The server must not change a Change Entry after it records it. The server must delete a Change Entry only as R2 and P8 give.

### Changefeed filters

Q1. A poll or a subscription can give a list of filter groups, in `filterGroups`. A change **matches** a filter group if the change matches each filter of the group. A Change Entry matches a list of filter groups if at least one change in the Change Entry matches at least one of the filter groups. The server must send each Change Entry that matches, with all of its changes, and must send no other Change Entry.

Q2. The **default `archived` filter** holds `check` `either` and `values` `[false]`. If a filter group has no `archived` member, the server must apply the default `archived` filter for that group. If the input gives no filter groups, or an empty list, the server must use one filter group that holds only the default `archived` filter.

Q3. A filter matches a change if at least one of its values matches, as these rules give:

   - `type`: a change to a Node matches if the Type of the Node is the value. A change to an Edge matches if the Type of the Tail Node or of the Head Node is the value
   - `watcher`: a change to a Node matches if the watchers of the Node include the value, or if the value is no value and the Node has no watchers. A change to an Edge matches if the Tail Node or the Head Node matches in this way
   - `status` and `assignee`: a change to a Node matches as Q6 gives. A change to an Edge never matches
   - `archived`: a change to a Node matches as Q6 gives. A change to an Edge matches if the Tail Node or the Head Node matches as Q6 gives

Q4. For the `watcher` filter, the server must use the watchers that the Node has when the server checks the filter. If the Node is deleted at that time, the server must use the watchers in the `before` of the change that deleted the Node.

Q5. For each change to an Edge, the server must keep, with the Change Entry, the Type of each end Node, and the value of `archived` of each end Node before and after the write. The server must not put these values in an output.

Q6. The server checks a `StateFilter` against a Node in these steps:

   1. Take the **before values**, which are the values of `beforeValues`, or the values of `values` if the filter has no `beforeValues`. Take the **after values** in the same way, from `afterValues` or `values`
   2. The Node **matches before** if the Node existed before the write, and its value before the write is one of the before values. The Node **matches after** if the Node exists after the write, and its value after the write is one of the after values. The value of a Node is the value of its `status` Field, of its `assignee` Field (which can be no value), or of its `archived` member, as the name of the filter gives
   3. The filter matches the Node if:
      - for `before`: the Node matches before
      - for `after`: the Node matches after
      - for `both`: the Node matches before and matches after
      - for `either`: the Node matches before or matches after

   For a change to a Node, the server uses the Node in `before` and in `after`. Thus the creation of a Node never matches `before` or `both`, and the deletion of a Node never matches `after` or `both`.

   For a change to an Edge, including the creation and the deletion of the Edge, the server uses each end Node as it was before the write and as it is after the write. By step 2, an end Node doesn't match at a time when it doesn't exist (e.g. a Node that the same write deletes). Thus, with `after`, the deletion of an Edge matches if at least one end Node matches after the write.

### Expose Changefeed

K1. Expose Changefeed is a setting of the server configuration (M1). No operation changes the server configuration.

K2. The server must record Change Entries and give versions whether Expose Changefeed is `true` or `false`.

K3. If Expose Changefeed is `false`, `Poll` and `Subscribe` must refuse with `changefeed_disabled`. Each other operation must run as it does when Expose Changefeed is `true`.

### Retention of Change Entries

R1. The **size** of a Change Entry is the number of bytes that the extended specification gives for it. The **count** is the number of the Change Entries of all Products together, and the **total size** is the sum of their sizes.

R2. While Delete Old Change Entries is `true`, and the count is above the Change Entry Count Target or the total size is above the Change Entry Size Target, the server must delete Change Entries until the count and the total size are within both targets. The server must delete the oldest Change Entries first:

   - In each Changefeed, the server must delete the Change Entries in ascending order of Change ID
   - Across Changefeeds, the server must delete the Change Entries in ascending order of `time`, and Change Entries with the same `time` in any order

R3. The server may run the deletions of R2 after the write that made the count or the total size go above a target, and not atomically with that write. So the count and the total size can be above a target for a time. This document doesn't define how soon after the write the server must run the deletions.

R4. While Delete Old Change Entries is `false`, the server must keep each Change Entry until it deletes the Changefeed of a deleted Product (P8).

### Server configuration

M1. The server must read each setting of the table below from its environment variable when the server starts. If the environment variable isn't set, the server must use the default value. If the environment variable has a value that isn't valid, the server must not start. A boolean is valid if it is `true` or `false`. An integer is valid if it is one or more ASCII decimal digits, and its value is in its range.

| Setting                    | Environment variable                 | Schema type | Default        | Comments                                                                    |
| -------------------------- | ------------------------------------ | ----------- | -------------- | --------------------------------------------------------------------------- |
| Expose Changefeed          | `NODOPLEX_EXPOSE_CHANGEFEED`         | boolean     | `true`         | K1 to K3                                                                    |
| Change Entry Count Target  | `NODOPLEX_CHANGE_ENTRY_COUNT_TARGET` | integer     | `1000000`      | at least 1. Above this count, the server deletes the oldest Change Entries (R2) |
| Change Entry Size Target   | `NODOPLEX_CHANGE_ENTRY_SIZE_TARGET`  | integer     | `1073741824`   | at least 1. Above this total size, in bytes, the server deletes the oldest Change Entries (R2) |
| Delete Old Change Entries  | `NODOPLEX_DELETE_OLD_CHANGE_ENTRIES` | boolean     | `true`         | `false` turns off the deletions of R2                                       |

### Concurrency

These operations are the **writes**:

- `CreateProduct`
- `CreateNode`, `UpdateNode`, `ArchiveNode`, `UnarchiveNode`, `DeleteNode`, `WatchNode`, and `UnwatchNode`
- `CreateEdge`, `UpdateEdge`, and `DeleteEdge`

These operations are the **reads**:

- `ListProducts`
- `GetNode`, `ListNodes`, and `GetNextWork`
- `GetEdge`, `GetEdgesFrom`, `GetEdgesTo`, `GetHeadNode`, and `GetTailNode`
- `GetLatestChangeId` and `Poll`
- steps 1 to 5 of `Subscribe`, and each run of step 6 of `Subscribe`

S1. The server must run the writes that name the same slug one at a time.

S2. The changes that a write makes, and the Change Entry that it records, must be atomic. If a write refuses, the server must keep none of its changes.

S3. After the server tells a client that a write succeeded, the changes of the write must remain after a restart of the server.

S4. If a write succeeds before a read starts, the read must read a state that includes the changes of that write.

S5. A read must compute its whole output from one state of the Product. `ListProducts` must compute the data of each Product from one state of that Product.

### Errors

E1. If the server can't complete an operation for a reason that has no other `code`, the server must refuse with `internal_error`.

E2. The `message` of an `internal_error` must not contain any of these:

   - the text of the error that caused the `internal_error`
   - a host name that the server uses
   - a database name that the server uses
   - a user name that the server uses

### Client recommendations

CR1. For each Changefeed that a client reads, the client should store a Change ID, and should send it as the start Change ID of its next poll or subscription. The client should store the Change ID of a Change Entry only after it has processed that Change Entry.

CR2. If a poll returns fewer Change Entries than the limit, then after the client has processed all of them, the client should store the `latestChangeId` of the response.

## Operations

Each operation is a sequence of steps. Each operation except `ListProducts` takes a slug as an input, besides the members of its input table.

The server must make these checks on each request, in this order, before step 1, and must stop at the first check that refuses:

1. If the request carries no user ID, or a user ID that isn't a valid `UserId`, refuse with `invalid_request`
2. If the operation takes a slug, and the slug isn't a valid slug, refuse with `invalid_request`
3. If the input gives a member that the input table of the operation doesn't name, or a value of a schema in the input has a member that the schema doesn't name, refuse with `invalid_request`
4. If a member of the input has a value outside its schema type or its range, refuse with `invalid_request`. This check also applies to each member of each value of a schema in the input (e.g. each `StateFilter`), and to each constraint that the description of a schema gives (e.g. a `FilterGroup` must have at least one member). A value of schema type `Fields` passes this check if it holds at most one value for each Field name. The steps of the operation check its Fields
5. If the input doesn't give a member that the input table marks required, refuse with `invalid_request`
6. If the operation is neither `ListProducts` nor `CreateProduct`, and no Product that exists has the slug, refuse with `not_found`. For `Poll`, `Subscribe`, and `GetLatestChangeId`, a deleted Product whose Changefeed the server still keeps counts as a Product that exists

Then the server must run the steps of the operation in order, and must stop at the first refusal. The `code` of a refusal must be the code that the check, the rule, or the step names. If a request reaches a step that returns, the request succeeds.

### Procedures

The steps of the operations use these procedures.

To **check the Fields** of an input for a Type, the server must run these steps:

1. If `fields` names a Field that the Type doesn't have (N4), refuse with `invalid_request`
2. If `fields` names a Field that the server sets (e.g. `product`), refuse with `invalid_request`. For this step, the client sets `status`
3. If `fields` gives a Field a value outside the schema type or the range of that Field, refuse with `invalid_request`
4. If `fields` gives no value to a required Field, refuse with `invalid_request`
5. If the operation creates a Node, and `fields` doesn't name a required Field that the client sets, and the Comments column gives no value for that case, refuse with `invalid_request`

To **complete a write**, the server must run these steps on the state that the earlier steps of the operation made:

1. Compute the value of `archived` of each Node, as A2 gives
2. Compute the status of each Node that isn't a leaf, as T3 gives. Leave the status of each leaf as the earlier steps of the operation left it (T5)
3. If a leaf whose status is `done` is blocked, refuse with `done_leaf_blocked` (T6)
4. If C4 gives that the write records a Change Entry:
   1. Take the latest Change ID of the Product, plus 1, as the new Change ID. Store it as the latest Change ID of the Product
   2. Set the `version` of each Node that the write creates, or whose state the write changes, to the new Change ID
   3. Record a Change Entry with the new Change ID as `changeId`, the time that C7 gives as `time`, the user ID of the request as `userId`, and the changes that C5 gives as `changes`

### Products

`CreateProduct`, `ListProducts`.

#### `CreateProduct`

Input:

| Member   | Schema type | Required | Comments                     |
| -------- | ----------- | -------- | ---------------------------- |
| `fields` | `Fields`    | yes      | the Fields of the Product Node |

Output: the new Product `Node`.

Steps:

1. Check the Fields of the input for the Type `product`
2. If a Product that exists, or a deleted Product, has the slug, refuse with `slug_taken`
3. Atomically:
   1. Create a Node with a new `id`, the `type` `product`, the Fields of the input, no watchers, and no archived mark. Set its `product` Field and its `slug` Field to the slug. Set each Field that the input doesn't name to the value that the Comments column of the Fields table gives, if the column gives one
   2. Create the Changefeed of the Product, with a latest Change ID of 0
   3. Complete the write. The Change Entry that it records has the Change ID 1, and holds the creation of the Product Node
4. Return the Node

#### `ListProducts`

Input:

| Member     | Schema type | Required | Comments                                   |
| ---------- | ----------- | -------- | ------------------------------------------ |
| `archived` | boolean[]   | no       | at least 1 value. If absent, `[false]`     |

Output: a `ProductList`.

Steps:

1. Select the Product Node of each Product that exists. Leave out each deleted Product
2. Keep each Product Node whose value of `archived` is one of the values of `archived`
3. Return a `ProductList` that holds the Product Nodes that step 2 kept, in code point order of their slugs

### Nodes

`CreateNode`, `GetNode`, `ListNodes`, `GetNextWork`, `UpdateNode`, `ArchiveNode`, `UnarchiveNode`, `DeleteNode`, `WatchNode`, `UnwatchNode`.

#### `CreateNode`

Input:

| Member    | Schema type   | Required | Comments                   |
| --------- | ------------- | -------- | -------------------------- |
| `type`    | `NodeType`    | yes      |                            |
| `parents` | `ParentRef`[] | yes      | at least 1 value           |
| `fields`  | `Fields`      | yes      | the Fields of the new Node |

Output: the new `Node`.

Steps:

1. If the value of `type` is `product`, refuse with `invalid_request`
2. If 2 values of `parents` have the same `id`, refuse with `invalid_request`
3. If the value of `type` is `vision`, and `parents` has more than 1 value, refuse with `invalid_request`
4. Check the Fields of the input for the value of `type`
5. For each value of `parents`:
   1. If no Node in the Product has the `id`, refuse with `invalid_reference`
   2. If that Node doesn't have the Parent Type of the value of `type`, refuse with `invalid_reference`
6. Atomically:
   1. Create a Node with a new `id`, the value of `type`, the Fields of the input, no watchers, and no archived mark. Set its `product` Field to the slug. Set each Field that the input doesn't name to the value that the Comments column of the Fields table gives, if the column gives one
   2. For each value of `parents`, create a Hierarchical Edge with a new `id`, from the new Node to the Parent. Give it the `priority` of the value, or the priority that G7 gives if the value has no `priority`
   3. Complete the write
7. Return the Node

#### `GetNode`

Input:

| Member            | Schema type | Required | Comments              |
| ----------------- | ----------- | -------- | --------------------- |
| `id`              | uuid        | yes      |                       |
| `includeArchived` | boolean     | no       | If absent, `false`    |

Output: the `Node`.

Steps:

1. If no Node in the Product has the `id`, refuse with `not_found`
2. If the Node is archived, and the value of `includeArchived` is `false`, refuse with `not_found`
3. Return the Node

#### `ListNodes`

Input:

| Member            | Schema type              | Required | Comments                                                       |
| ----------------- | ------------------------ | -------- | -------------------------------------------------------------- |
| `type`            | `NodeType`[]             | no       | at least 1 value                                               |
| `status`          | `Status`[]               | no       | at least 1 value                                               |
| `assignee`        | (`UserId`, nullable)[]   | no       | at least 1 value                                               |
| `watcher`         | (`UserId`, nullable)[]   | no       | at least 1 value                                               |
| `archived`        | boolean[]                | no       | at least 1 value. If absent, `[false]`                         |
| `blocked`         | boolean[]                | no       | at least 1 value                                               |
| `childrenOf`      | uuid[]                   | no       | at least 1 value                                               |
| `descendantsOf`   | uuid[]                   | no       | at least 1 value                                               |
| `parentsOf`       | uuid[]                   | no       | at least 1 value                                               |
| `ancestorsOf`     | uuid[]                   | no       | at least 1 value                                               |
| `dependentsOf`    | uuid[]                   | no       | at least 1 value                                               |
| `prerequisitesOf` | uuid[]                   | no       | at least 1 value                                               |
| `search`          | string                   | no       | at least 1 character                                           |
| `sort`            | `lastUpdated`, `priority` | no      | If absent, `lastUpdated`                                       |
| `direction`       | `descending`, `ascending` | no      | only with the sort `lastUpdated`. If absent, `descending`      |
| `startAfterKey`   | integer                  | no       | the sort key of the last Node of the previous page             |
| `startAfterId`    | uuid                     | no       | the `id` of the last Node of the previous page                 |
| `limit`           | integer                  | no       | from 1 to 200, both inclusive. If absent, 50                   |

Each member from `type` to `search` is a filter. A Node matches a filter if the Node matches at least one value of the filter, as the table below gives:

| Filter            | A Node matches the value if                                                                                  |
| ----------------- | ------------------------------------------------------------------------------------------------------------ |
| `type`            | the Type of the Node is the value                                                                            |
| `status`          | the value of the `status` Field of the Node is the value                                                     |
| `assignee`        | the value of the `assignee` Field of the Node is the value, or the value is no value and the Node has no assignee |
| `watcher`         | the watchers of the Node include the value, or the value is no value and the Node has no watchers           |
| `archived`        | the value of `archived` of the Node is the value                                                             |
| `blocked`         | the value is `true` and the Node is blocked, or the value is `false` and the Node isn't blocked              |
| `childrenOf`      | the Node is a Child of the Node that has the value as its `id`                                               |
| `descendantsOf`   | the Node is a descendant of the Node that has the value as its `id`                                          |
| `parentsOf`       | the Node is a Parent of the Node that has the value as its `id`                                              |
| `ancestorsOf`     | the Node is an ancestor of the Node that has the value as its `id`                                           |
| `dependentsOf`    | the Node is a Dependent of the Node that has the value as its `id`                                           |
| `prerequisitesOf` | the Node is a Prerequisite of the Node that has the value as its `id`                                        |
| `search`          | the `title` or the `description` of the Node contains the value, ignoring case (e.g. `auth` matches "OAuth login") |

If the value of a filter from `childrenOf` to `prerequisitesOf` is the `id` of no Node in the Product, that value matches no Node. These filters use each Edge of the Product, including each Edge that has an archived Node at either end.

The **sort key** of a Node is:

- for the sort `lastUpdated`: the version of the Node
- for the sort `priority`: the priority of the Hierarchical Edge from the Node to the Node that `childrenOf` names

The sorted order is:

- for the sort `lastUpdated` and the direction `descending`: descending order of sort key, and then id order
- for the sort `lastUpdated` and the direction `ascending`: ascending order of sort key, and then id order
- for the sort `priority`: ascending order of sort key, and then id order

Output: a `NodePage`.

Steps:

1. If the value of `sort` is `priority`, and `childrenOf` doesn't have exactly 1 value, refuse with `invalid_request`
2. If the input gives `direction`, and the value of `sort` is `priority`, refuse with `invalid_request`
3. If the input gives exactly one of `startAfterKey` and `startAfterId`, refuse with `invalid_request`
4. Select each Node in the Product that matches each filter of the input. For each filter that the input doesn't give, use its default. The default of `archived` is `[false]`. The default of each other filter matches each Node
5. Sort the selected Nodes in the sorted order
6. If the input gives `startAfterKey` and `startAfterId`, discard each Node whose sort key and `id` come at or before the value of `startAfterKey` and the value of `startAfterId` in the sorted order
7. Keep at most as many Nodes as the value of `limit`, from the start of the sorted order
8. Return a `NodePage` with the Nodes that step 7 kept, in the sorted order, and the sort key of the last of those Nodes as `lastKey`

#### `GetNextWork`

Input: none.

Output: a `NextWork`.

Steps:

1. Find the next available leaf of the Product, as W2 gives
2. Return a `NextWork` that holds the next available leaf as `node`, or that has no `node` if step 1 found none

#### `UpdateNode`

Input:

| Member    | Schema type | Required | Comments                                                                                       |
| --------- | ----------- | -------- | ---------------------------------------------------------------------------------------------- |
| `id`      | uuid        | yes      |                                                                                                |
| `version` | integer     | no       | the version of the Node that the client last read (V2)                                         |
| `fields`  | `Fields`    | yes      | names at least 1 Field. A Field with a value sets that value. A Field with no value removes the value of that Field |

Output: the `Node`.

Steps:

1. If no Node in the Product has the `id`, refuse with `not_found`
2. Check the Fields of the input for the Type of the Node
3. If `fields` names a tracked Field, and the input doesn't give `version`, refuse with `invalid_request`
4. If the input gives `version`, and the version of the Node isn't the value of `version`, refuse with `version_conflict`
5. If `fields` names `status`, and the Node isn't a leaf, refuse with `status_is_derived`
6. Atomically:
   1. For each Field that `fields` names, set the Field of the Node to the value that `fields` gives, or remove the value of the Field if `fields` gives no value. Leave each other Field as it was
   2. Complete the write
7. Return the Node

#### `ArchiveNode`

Input:

| Member    | Schema type | Required | Comments                                               |
| --------- | ----------- | -------- | ------------------------------------------------------ |
| `id`      | uuid        | yes      |                                                        |
| `version` | integer     | yes      | the version of the Node that the client last read (V2) |

Output: the `Node`.

Steps:

1. If no Node in the Product has the `id`, refuse with `not_found`
2. If the version of the Node isn't the value of `version`, refuse with `version_conflict`
3. If the Node carries the archived mark, return the Node
4. Atomically:
   1. Place the archived mark on the Node
   2. Complete the write
5. Return the Node

#### `UnarchiveNode`

Input: as for `ArchiveNode`.

Output: the `Node`.

Steps:

1. If no Node in the Product has the `id`, refuse with `not_found`
2. If the version of the Node isn't the value of `version`, refuse with `version_conflict`
3. If the Node doesn't carry the archived mark, return the Node
4. Atomically:
   1. Remove the archived mark from the Node
   2. Complete the write
5. Return the Node

#### `DeleteNode`

Input: as for `ArchiveNode`.

Output: none.

Steps:

1. If no Node in the Product has the `id`, refuse with `not_found`
2. If the version of the Node isn't the value of `version`, refuse with `version_conflict`
3. If an Edge points to the Node, refuse with `has_incoming_edges`
4. Atomically:
   1. Delete each Edge that points from the Node
   2. Delete the Node. If the Node is the Product Node, the Product becomes a deleted Product (P7)
   3. Complete the write
5. Return

#### `WatchNode`

Input:

| Member | Schema type | Required |
| ------ | ----------- | -------- |
| `id`   | uuid        | yes      |

Output: the `Node`.

Steps:

1. If no Node in the Product has the `id`, refuse with `not_found`
2. If the watchers of the Node don't include the user ID of the request, add it to the watchers
3. Return the Node

#### `UnwatchNode`

Input: as for `WatchNode`.

Output: the `Node`.

Steps:

1. If no Node in the Product has the `id`, refuse with `not_found`
2. If the watchers of the Node include the user ID of the request, remove it from the watchers
3. Return the Node

### Edges

`CreateEdge`, `GetEdge`, `GetEdgesFrom`, `GetEdgesTo`, `GetHeadNode`, `GetTailNode`, `UpdateEdge`, `DeleteEdge`.

#### `CreateEdge`

Input:

| Member     | Schema type | Required | Comments                                                         |
| ---------- | ----------- | -------- | ---------------------------------------------------------------- |
| `kind`     | `EdgeKind`  | yes      |                                                                  |
| `tailId`   | uuid        | yes      |                                                                  |
| `headId`   | uuid        | yes      |                                                                  |
| `priority` | integer     | no       | only for a Hierarchical Edge. If absent, G7 gives the priority   |
| `version`  | integer     | yes      | the version of the Tail Node that the client last read (V2)      |

Output: the new `Edge`.

Steps:

1. If the value of `kind` is `dependency`, and the input gives `priority`, refuse with `invalid_request`
2. If no Node in the Product has the value of `tailId`, refuse with `invalid_reference`
3. If no Node in the Product has the value of `headId`, refuse with `invalid_reference`
4. If the version of the Tail Node isn't the value of `version`, refuse with `version_conflict`
5. If the value of `kind` is `hierarchical`, and the Head Node doesn't have the Parent Type of the Type of the Tail Node, refuse with `invalid_reference`
6. If an Edge connects the Tail Node and the Head Node, in either direction, refuse with `already_connected`
7. If the new Edge would form a cycle (Y1, Y2), refuse with `cycle`
8. Atomically:
   1. Create the Edge with a new `id`. If the Edge is a Hierarchical Edge, give it the value of `priority`, or the priority that G7 gives if the input doesn't give `priority`
   2. Complete the write
9. Return the Edge

#### `GetEdge`

Input:

| Member            | Schema type | Required | Comments           |
| ----------------- | ----------- | -------- | ------------------ |
| `id`              | uuid        | yes      | the `id` of the Edge |
| `includeArchived` | boolean     | no       | If absent, `false` |

Output: the `Edge`.

Steps:

1. If no Edge in the Product has the `id`, refuse with `not_found`
2. If the value of `includeArchived` is `false`, and the Tail Node or the Head Node is archived, refuse with `not_found`
3. Return the Edge

#### `GetEdgesFrom`

Input:

| Member            | Schema type | Required | Comments           |
| ----------------- | ----------- | -------- | ------------------ |
| `id`              | uuid        | yes      | the `id` of the Node |
| `includeArchived` | boolean     | no       | If absent, `false` |

Output: an `EdgeList`.

Steps:

1. If no Node in the Product has the `id`, refuse with `not_found`
2. If the Node is archived, and the value of `includeArchived` is `false`, refuse with `not_found`
3. Select each Edge that points from the Node. If the value of `includeArchived` is `false`, leave out each Edge whose Head Node is archived
4. Return an `EdgeList` that holds the selected Edges

#### `GetEdgesTo`

Input: as for `GetEdgesFrom`.

Output: an `EdgeList`.

Steps:

1. If no Node in the Product has the `id`, refuse with `not_found`
2. If the Node is archived, and the value of `includeArchived` is `false`, refuse with `not_found`
3. Select each Edge that points to the Node. If the value of `includeArchived` is `false`, leave out each Edge whose Tail Node is archived
4. Return an `EdgeList` that holds the selected Edges

#### `GetHeadNode`

Input: as for `GetEdge`.

Output: a `Node`.

Steps:

1. Run steps 1 and 2 of `GetEdge`
2. Return the Head Node of the Edge

#### `GetTailNode`

Input: as for `GetEdge`.

Output: a `Node`.

Steps:

1. Run steps 1 and 2 of `GetEdge`
2. Return the Tail Node of the Edge

#### `UpdateEdge`

Input:

| Member     | Schema type | Required | Comments                                                    |
| ---------- | ----------- | -------- | ----------------------------------------------------------- |
| `id`       | uuid        | yes      | the `id` of the Edge                                        |
| `priority` | integer     | yes      | the new priority                                            |
| `version`  | integer     | yes      | the version of the Tail Node that the client last read (V2) |

Output: the `Edge`.

Steps:

1. If no Edge in the Product has the `id`, refuse with `not_found`
2. If the version of the Tail Node isn't the value of `version`, refuse with `version_conflict`
3. If the Edge is a Dependency Edge, refuse with `invalid_request`
4. If the priority of the Edge is the value of `priority`, return the Edge
5. Atomically:
   1. Set the priority of the Edge to the value of `priority`
   2. Complete the write
6. Return the Edge

#### `DeleteEdge`

Input:

| Member    | Schema type | Required | Comments                                                    |
| --------- | ----------- | -------- | ----------------------------------------------------------- |
| `id`      | uuid        | yes      | the `id` of the Edge                                        |
| `version` | integer     | yes      | the version of the Tail Node that the client last read (V2) |

Output: none.

Steps:

1. If no Edge in the Product has the `id`, refuse with `not_found`
2. If the version of the Tail Node isn't the value of `version`, refuse with `version_conflict`
3. If the Edge is a Hierarchical Edge, and it is the only Hierarchical Edge that points from its Tail Node, refuse with `parent_required`
4. Atomically:
   1. Delete the Edge
   2. Complete the write
5. Return

### Changefeed

`GetLatestChangeId`, `Poll`, `Subscribe`.

#### `GetLatestChangeId`

Input: none.

Output: a `LatestChangeId`.

Steps:

1. Return a `LatestChangeId` that holds the latest Change ID of the Product

#### `Poll`

Input:

| Member          | Schema type     | Required | Comments                                |
| --------------- | --------------- | -------- | --------------------------------------- |
| `startChangeId` | integer         | yes      | 0 or more. Exclusive                    |
| `limit`         | integer         | yes      | from 1 to 1000, both inclusive          |
| `filterGroups`  | `FilterGroup`[] | no       |                                         |

Output: a `ChangePage`.

Steps:

1. If Expose Changefeed is `false`, refuse with `changefeed_disabled`
2. If the value of `startChangeId` is higher than the latest Change ID of the Product, refuse with `change_out_of_range`
3. If the value of `startChangeId` is higher than 0, and the server has deleted a Change Entry of the Product whose Change ID is higher than the value of `startChangeId`, refuse with `change_entries_deleted`
4. Select each Change Entry of the Product that the server keeps, and whose Change ID is higher than the value of `startChangeId`
5. Keep each selected Change Entry that matches the filter groups of the input (Q1, Q2)
6. Sort the kept Change Entries in ascending order of Change ID. Keep at most as many Change Entries as the value of `limit`, from the start of that order
7. Return a `ChangePage` with the Change Entries that step 6 kept, and the latest Change ID of the Product

If `startChangeId` is 0, step 4 selects each Change Entry of the Product that the server keeps.

#### `Subscribe`

Input:

| Member          | Schema type     | Required | Comments                                                        |
| --------------- | --------------- | -------- | --------------------------------------------------------------- |
| `startChangeId` | integer         | no       | 0 or more. Exclusive. If absent, the latest Change ID of the Product |
| `filterGroups`  | `FilterGroup`[] | no       |                                                                 |

Output: a subscription, on which the server pushes `ChangeEntry` values.

Steps:

1. If Expose Changefeed is `false`, refuse with `changefeed_disabled`
2. Set the **start Change ID** to the value of `startChangeId`, or to the latest Change ID of the Product if the input doesn't give `startChangeId`
3. If the start Change ID is higher than the latest Change ID of the Product, refuse with `change_out_of_range`
4. If the start Change ID is higher than 0, and the server has deleted a Change Entry of the Product whose Change ID is higher than the start Change ID, refuse with `change_entries_deleted`
5. Open the subscription. Set the **position** to the start Change ID
6. For each Change Entry of the Product whose Change ID is higher than the position, in ascending order of Change ID:
   1. If the Change Entry matches the filter groups of the input (Q1, Q2), push the Change Entry
   2. Set the position to the Change ID of the Change Entry
7. Wait until the server records a Change Entry of the Product whose Change ID is higher than the position. Then run step 6 again

The server must end an open subscription in each of these cases:

- When the client ends the subscription
- When the server deletes a Change Entry of the Product whose Change ID is higher than the position. The server must end the subscription with `change_entries_deleted`
- When the server deletes the Changefeed of a deleted Product (P8). The server must end the subscription with `not_found`

## Decisions left to the extended specification

The extended specification must define each of these:

- the transport, and how a request names the operation, the slug, the user ID, and each member of the input
- how each schema type, each schema, and each value of schema type `Fields` is written
- when 2 values of schema type object are the same
- how an input gives a member or a Field with no value
- how a refusal returns its `code` and its `message`
- how a client opens a subscription, how the server pushes each Change Entry, how either side ends a subscription, and how the server gives the `code` with which it ends a subscription
- which strings are valid slugs (P1)
- the size of a Change Entry, in bytes (R1)
- how the `search` filter of `ListNodes` ignores case
- the code of the refusal of an input above a size limit of the implementation

The extended specification may define more Fields (N5), more read operations, and other kinds of search (e.g. full-text, semantic, or vector search).

## Appendix A: refusal codes

The table below gives each refusal that a check before step 1, a rule, or an operation step names.

| `code`                   | Raised by                                                                                         | Condition                                                                                                     |
| ------------------------ | ------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| `invalid_request`        | every operation                                                                                   | the request fails a check before step 1, or a step that names this code                                       |
| `not_found`              | every operation except `ListProducts` and `CreateProduct`, and an open subscription               | the Product, the Node, or the Edge that the request names doesn't exist, or the read leaves it out because it is archived (A4) |
| `slug_taken`             | `CreateProduct`                                                                                   | a Product that exists, or a deleted Product, has the slug (P3)                                                |
| `invalid_reference`      | `CreateNode`, `CreateEdge`                                                                        | an `id` in the input is the `id` of no Node in the Product, or of a Node whose Type G2 forbids                |
| `already_connected`      | `CreateEdge`                                                                                      | an Edge already connects the 2 Nodes (G5)                                                                     |
| `cycle`                  | `CreateEdge`                                                                                      | the new Edge would form a cycle (Y1, Y2)                                                                      |
| `parent_required`        | `DeleteEdge`                                                                                      | the Hierarchical Edge is the only Hierarchical Edge that points from its Tail Node (G3)                       |
| `has_incoming_edges`     | `DeleteNode`                                                                                      | an Edge points to the Node                                                                                    |
| `version_conflict`       | `UpdateNode`, `ArchiveNode`, `UnarchiveNode`, `DeleteNode`, `CreateEdge`, `UpdateEdge`, `DeleteEdge` | the version of the Node isn't the version in the input (V3)                                                   |
| `status_is_derived`      | `UpdateNode`                                                                                      | the input sets the status of a Node that isn't a leaf (T1)                                                    |
| `done_leaf_blocked`      | each operation that completes a write                                                             | after the write, a leaf whose status is `done` would be blocked (T6)                                          |
| `priority_out_of_range`  | `CreateNode`, `CreateEdge`                                                                        | the largest priority of the other Hierarchical Edges to the Parent is 2^63 - 1 (G7)                          |
| `changefeed_disabled`    | `Poll`, `Subscribe`                                                                               | Expose Changefeed is `false` (K3)                                                                             |
| `change_out_of_range`    | `Poll`, `Subscribe`                                                                               | the start Change ID is higher than the latest Change ID of the Product                                        |
| `change_entries_deleted` | `Poll`, `Subscribe`, and an open subscription                                                     | the server has deleted a Change Entry whose Change ID is higher than the start Change ID, or than the position of the subscription |
| `internal_error`         | every operation                                                                                   | the server can't complete the operation for a reason that has no other `code` in this table (E1)              |
