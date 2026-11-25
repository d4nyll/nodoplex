# Mental Model

> This document is a high-level mental model of Nodoplex. The [base specification](01_base-specification.md) is an implementation-agnostic specification that expands on this mental model. The base specification defines these in more detail:
>
> - exact rules and constraints
> - error handling
> - data schemas (including errors)
>
> An extended specification extends the base specification with the rules specific to one technology stack (e.g. [HTTP and JSON](02_extended-specification.md)).

Nodoplex is a server that records details about work on a product, whether that's past work, in-progress work, or work planned for the future.

Nodoplex stores the details of each product as a separate directed acyclic graph (DAG). Nodoplex represents each item of work as a **Node**, and each relationship between 2 items as a directed **Edge**.

Clients read and write Nodes and Edges. Nodoplex refuses any write that violates a rule in the base specification or in the extended specification that applies.

For each product, Nodoplex also keeps a **Changefeed**, which is a log of the changes to the product's graph. Each item in the log is a **Change Entry**. A client can read the Changefeed to get the changes made since the client last read the Changefeed, instead of reading the whole graph again.

## Users

Every request includes the **user ID** of the user who sends the request. Nodoplex accepts that user ID without a check, and doesn't authenticate or authorize anyone. Nodoplex uses the user ID to record who made each change.

## Nodes

A Node represents one item of the work on a product.

When a client creates a Node:

- the client chooses the Node's **Type**. The Type is immutable.
- Nodoplex generates an **ID** and assigns it to the Node. The ID is immutable.

The Node Types form a hierarchy. Each Type except Product breaks down the Type one level above it. Each arrow points from a Type to the broader Type that it breaks down:

```
(broadest)                                                  (narrowest)
Product <- Vision <- Goal <- Feature <- Requirement <- Task <- SubTask
```

- **Product**: represents the product itself (e.g. "A language-learning app"). Each product has exactly one Product Node, which is the root Node of the product's graph
- **Vision**: represents an overarching purpose of the product (e.g. "Provide a free application to teach and track language-learning for users") and/or an aspiration of the product (e.g. "Make learning a language fun")
- **Goal**: represents an outcome that the product must achieve (e.g. "Learners stay motivated to study")
- **Feature**: represents a capability of the product (e.g. "A gamification system")
- **Requirement**: represents a condition that the capability of a Feature must meet (e.g. "A learner earns points for each lesson that the learner completes")
- **Task**: represents a unit of work that fulfills the condition of a Requirement (e.g. "Develop mechanism for awarding points when a learner completes a lesson")
- **SubTask**: represents a part of the work that a Task represents (e.g. "Research established gamification patterns")

### Fields

A Node stores in **Fields** the details of the work and the metadata of the Node. Each Field is defined for exactly one Node Type. Different Types can each have a Field with the same name, and those Fields are separate.

Each Field has these properties:

- which Node Type has the Field
- what the Field means
- which values are valid for the Field
- whether the Field is required or optional
- whether the Field is tracked or untracked

A write that changes a **tracked** Field produces a Change Entry. A write that changes only **untracked** Fields doesn't. The base specification and the extended specification define every Field, tracked or untracked. Clients don't define Fields. Clients use untracked Fields to store data for their own purposes.

Every Type has these tracked Fields:

- `title`, which is required
- `description`, which is optional
- `outputs`, which holds a list of **Outputs**. The list can be empty. An Output represents a result of the work that the Node represents (e.g. a Git branch, a Slack message). To add an Output to a Node, a client updates the `outputs` Field of the Node.
- `status`, which holds the progress of the work
- `assignee`, which holds the user ID of one user, or no user ID

A Product also has a required `slug` Field, which is immutable.

## Edges

When a client creates an Edge, Nodoplex generates an **ID** and assigns it to the Edge. The ID is immutable.

An Edge points from its **Tail Node** to its **Head Node**. At most one Edge connects any 2 Nodes, whatever the kind or direction of the Edge. Every Edge is one of these kinds:

- A **Hierarchical Edge** points from a **Child** to its **Parent**.
  - A Product has no Parent. A Vision has exactly one Parent, the Product of its graph. Every other Node has at least one Parent, and can have more than one. For example, a Task can have 2 Requirements as its Parents.
  - A Parent is always of the Type exactly one level above the Type of its Child. For example:
    - A Vision's Parent is the Product
    - A Goal's Parent is a Vision
    - A Task's Parent is a Requirement
    - A Feature can't be the Parent of a Task
  - Each Hierarchical Edge holds a `priority` number. The priority orders the Child among the other Children of the same Parent, and a lower number means a higher priority. A Node with 2 Parents has 2 priorities, one for each Parent.
  - 2 Children of the same Parent can have the same priority number
  - If a client creates a Hierarchical Edge without a priority, Nodoplex gives the Edge a priority number greater than the priority number of every other Hierarchical Edge to that Parent. If the Parent has no other Children, Nodoplex gives the Edge a default priority number.

- A **Dependency Edge** points from a **Dependent** to the **Prerequisite** that it depends on.
  - The 2 Nodes can be of the same or different Types. For example, a Task can depend on another Task, or on a Requirement.
  - The 2 Nodes can have different Parents
  - A Node can't depend on its own ancestors or descendants
  - Priorities and Dependency Edges are independent, and Nodoplex doesn't check one against the other. For example, Task B can depend on Task A even if B has a lower priority number than A under a Parent that they share.

An Edge holds:

- its ID
- its kind
- the ID of its Tail Node
- the ID of its Head Node
- its priority, if it's a Hierarchical Edge

### How a client creates, updates, and deletes Nodes and Edges

- A request to create a Node specifies the Parents of the new Node:
  - for a Product, no Parent
  - for a Vision, exactly one Parent, which is the Product
  - for a Node of any other Type, at least one Parent

  Nodoplex atomically creates the Node and a Hierarchical Edge from the new Node to each of its Parents.
- A client can update the Fields of a Node. An update changes only the Fields that the update names, and the other Fields keep their values.
- A client can create an Edge between 2 existing Nodes, and can delete an Edge
- If a Hierarchical Edge is the only Hierarchical Edge from its Child, Nodoplex refuses to delete that Edge. To move a Node to a different Parent, a client first creates the Hierarchical Edge to the new Parent, and then deletes the Hierarchical Edge to the old Parent.
- A client can update the priority of a Hierarchical Edge. The priority is the only part of an Edge that can change.
- While any Edge points to a Node, Nodoplex refuses to delete the Node.
- When a client deletes a Node, Nodoplex atomically deletes the Node and every Edge that points from the Node

## Products

One Nodoplex server can serve many products. Each product has its own graph, with one Product Node as its root Node.

- When a client creates a new product, Nodoplex creates:
  - a new graph with a single Product Node
  - a new Changefeed for that product. The first Change Entry in the Changefeed holds the creation of the Product.
- A client names a Product by its `slug`. Each slug is unique on the Nodoplex server. Nodoplex also refuses to create a Product with the slug of a deleted Product. Nodoplex refuses a slug that isn't valid. Apart from that check, Nodoplex doesn't interpret the characters of a slug. The base specification may define which strings are valid slugs, and the extended specification must define them.
- A client can list the Products. That request names no Product.
- Every other request about Nodes, Edges, or Change Entries names one Product by its slug, and Nodoplex reads and writes only the data of that Product
- Every Node is in the graph of exactly one Product. If a request names a Node that isn't in the graph of the Product that the request names, Nodoplex answers as if the Node doesn't exist.
- The Tail Node and the Head Node of an Edge are always in the same graph
- When a client deletes a Product, the deletion produces a Change Entry, as the deletion of any other Node does. Nodoplex keeps the Changefeed of the deleted Product for a set time, so that a client can still read the Change Entries of the Product, including the deletion. The base specification or the extended specification defines that time. Until that time ends, Nodoplex accepts these requests for the deleted Product:
  - poll
  - subscribe
  - a request that reads the latest Change ID

  Nodoplex answers every other request for the deleted Product as if the Product doesn't exist. When that time ends, Nodoplex deletes the Changefeed, and ends each open subscription to the Changefeed. Nodoplex then answers every request for the deleted Product as if the Product doesn't exist.

## Archiving

An archived Node represents work that someone wanted before, but no longer wants. To archive a Node, a client sends an archive request, and Nodoplex places an **archived mark** on the Node. To unarchive a Node, a client sends an unarchive request, and Nodoplex removes the archived mark. A client can archive a Node of any Type.

A Node is **archived** if:

- the Node carries the archived mark, or
- the Node has at least one Parent, and every path of Hierarchical Edges from the Node to the Product passes through a Node that carries the archived mark. Each path ends at the Product, so if the Product carries the archived mark, every Node in the Product's graph is archived.

For example, Product P has one Vision V, and Goals G1 and G2 each have V as their only Parent. A client archives G1, so G1 carries the archived mark. No other Node carries the archived mark.

- Feature F has G1 as its only Parent. The only path from F to the Product is F, G1, V, P. That path passes through G1, so F is archived.
- Feature E has 2 Parents, G1 and G2. The path E, G2, V, P passes through no Node that carries the archived mark, so E isn't archived.

Nodoplex stores only the archived marks, and uses the marks and the Parents of each Node to calculate whether the Node is archived. So a Node can become archived, or stop being archived, without a change to its own archived mark:

- When a client places an archived mark on a Node, each descendant of that Node can become archived.
- When a client removes an archived mark from a Node, each descendant of that Node can stop being archived.
- When a client creates a Hierarchical Edge, the Child of that Edge and each descendant of the Child can stop being archived.
- When a client deletes a Hierarchical Edge, the Child of that Edge and each descendant of the Child can become archived.

When a write makes a Node become archived or stop being archived, the Change Entry of the write holds an update of that Node. For example, when the client archives G1 in the example of Product P, the Change Entry holds at least an update of G1 and an update of Feature F. The Change Entry holds no update of Feature E, because E doesn't become archived.

By default, when a client reads Nodes or Edges, or lists Nodes, Nodoplex leaves out archived Nodes, and leaves out every Edge that has an archived Node at either end. For example, by default, if a client reads an archived Node by its ID, or reads the Edges of an archived Node, Nodoplex answers as if the Node doesn't exist. A request that reads or lists Nodes or Edges can ask Nodoplex to include the archived Nodes and their Edges.

A client can still write to an archived Node. For example, a client can:

- update its Fields
- add a Child to it
- create Edges to and from it

## Status

The `status` Field holds one of these values:

- `not_started`
- `in_progress`
- `done`

A **leaf** is a Node that has no Children, archived or not. A client sets the status of a leaf. Every other Node has a **derived status**. Nodoplex derives it from the statuses of the Node's Children and Prerequisites, and stores it in the Node's `status` Field. A client can't set a derived status.

A Node is **blocked** if at least one of its Prerequisites is neither `done` nor archived. An archived Prerequisite doesn't block its Dependent, so that work nobody wants doesn't stop another Node from being `done`.

Nodoplex derives the status of a Node that isn't a leaf in these steps:

1. Nodoplex leaves out the Children that carry the archived mark. Nodoplex counts the other Children, including a Child that is archived only because of the archived marks on its ancestors. So inside an archived subtree, Nodoplex still derives the status of each Node from its own Children.
2. Nodoplex checks the Children that are left against these rules, in order, and uses the first rule that applies:
   1. If no Children are left:
      - if the Node isn't blocked, the derived status is `done`
      - otherwise, the derived status is `in_progress`
   2. If every Child is `done`, and the Node isn't blocked, the derived status is `done`
   3. If every Child is `not_started`, the derived status is `not_started`
   4. Otherwise, the derived status is `in_progress`

For example, a Task with no SubTasks is a leaf, so a client can set its status. A Task with SubTasks has a derived status. A SubTask can't have Children, so a SubTask is always a leaf. If Requirement R depends on Requirement S, and S isn't archived, the derived status of R can be `done` only while S is `done`.

- A new Node is always a leaf. When a client creates a Node, the client can set the status of the Node. If the client doesn't set the status, the status is `not_started`.
- When a leaf gets its first Child (e.g. a new SubTask), the Node stops being a leaf. Nodoplex then replaces the status that a client set with a derived status. When the Node becomes a leaf again (e.g. after the deletion of its last SubTask), the Node keeps its last derived status until a client sets a new status.
- A change to the status of one Node can change the derived status of each Parent and each Dependent of that Node. Each of those changes can then change more derived statuses in the same way.
- A client can set the status of an archived leaf. Archiving a Node doesn't change the status of that Node.
- A leaf can be `not_started` or `in_progress`, even if it's blocked. A leaf can be `done` only if it isn't blocked. Nodoplex refuses any write that would leave a `done` leaf blocked, even if the leaf is archived. The leaf can be several Edges away from the Node that the write changes. Those writes include:
  - a write that indirectly makes a Prerequisite that isn't archived stop being `done` (e.g. through a change to a derived status, or a new Child of the Prerequisite)
  - a write after which a Prerequisite that isn't `done` stops being archived (e.g. a write that removes the archived mark from the Prerequisite or from an ancestor of the Prerequisite, or a write that gives the Prerequisite a new Parent)

  For example, Task B is a leaf, and Task A is another Task. Neither Task is archived.
  - If B depends on A:
    - A client can set B to `in_progress` while A is `not_started`
    - Nodoplex refuses to set B to `done` until A is `done`
    - While B is `done`, Nodoplex refuses to change the status of A from `done`. To change the status of A, the client can first delete the Dependency Edge from B to A, or set B to `in_progress` or `not_started`.
  - If B doesn't depend on A, B is `done`, and A isn't `done`, Nodoplex refuses to create a Dependency Edge from B to A
- A write can make a `done` Node that has a derived status blocked (e.g. a change to the status of a Prerequisite, a new Dependency Edge). Nodoplex accepts that write. Atomically with the write, Nodoplex derives the status of the Node again, so the Node stops being `done`. For example, Requirement R has Tasks, so the status of R is derived. R depends on Requirement S. R and S are both `done`, and neither is archived. If a client makes a write that changes S from `done` to `in_progress`, Nodoplex accepts the write, and changes R to `in_progress` atomically with the change to S.

## Cycles

Nodoplex refuses any write that would form a cycle of Nodes of either of these kinds:

- a cycle in which each Node is a Child or a Dependent of the next Node
- a cycle in which each Node is a Parent or a Dependent of the next Node

The diagrams in this section use these arrows:

```
───>  Hierarchical Edge, from a Child to its Parent
┄┄┄>  Dependency Edge, from a Dependent to its Prerequisite
```

### Cycles of Children and Dependents

Nodoplex refuses any write that would form a cycle in which each Node is a Child or a Dependent of the next Node, and the last Node is a Child or a Dependent of the first Node. One result of this rule is that a Node can't depend on its own descendants. This kind of cycle follows the direction of each Edge. Hierarchical Edges alone can't form this kind of cycle, so each such cycle includes at least one Dependency Edge. The Edge that would close the cycle can be of either kind.

For example, Requirement R depends on Task B, and Task B depends on Task A. A client tries to create a Hierarchical Edge from Task A to Requirement R:

```
Requirement R ┄┄┄┄┄┄┄┄> Task B
      ▲                    ┆
      │ new                ┆
      │                    ┆
   Task A <┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┘
```

The new Edge would form this cycle:

1. R is a Dependent of B
2. B is a Dependent of A
3. A would be a Child of R

So Nodoplex refuses to create that Hierarchical Edge.

These logical steps define whether a new Edge would form this kind of cycle. An implementation can use other steps that give the same result:

1. Nodoplex starts at the Head Node of the new Edge.
2. Nodoplex goes to each Parent and each Prerequisite of that Node. From each Node that it reaches, Nodoplex goes on in the same way.
3. If Nodoplex reaches the Tail Node of the new Edge, the new Edge would form this kind of cycle, and Nodoplex refuses the write.

Before each write, the graph has no cycle of this kind. Only a write that creates an Edge can form one, and the cycle must include the new Edge. So these steps find every cycle of this kind that a write would form.

For example, for the Hierarchical Edge from A to R, Nodoplex starts at R, which is the Head Node. Nodoplex goes to B (a Prerequisite of R), and then to A (a Prerequisite of B). A is the Tail Node, so Nodoplex refuses the write.

### Cycles of Parents and Dependents

Nodoplex refuses any write that would form a cycle in which each Node is a Parent or a Dependent of the next Node, and the last Node is a Parent or a Dependent of the first Node. One result of this rule is that a Node can't depend on its own ancestors. If no Node in such a cycle is archived, each Node in the cycle can be `done` only when the next Node is `done`. So no Node in the cycle can become `done`.

Nodoplex counts archived Nodes too. If Nodoplex didn't count them, a client could form this kind of cycle through an archived Node, and Nodoplex would then have to refuse the unarchive request for that Node.

For example, Requirement R is the Parent of Task T, and T depends on Task X. SubTask Z is a Child of X. None of these Nodes is archived. A client tries to create a Dependency Edge from Z to R:

```
Requirement R  <┄┄┄┄┄┄┄┄┄┄┄┄┄┐
      ▲                      ┆
      │                      ┆
   Task T                    ┆ new
      ┆                      ┆
      ▼                      ┆
   Task X                    ┆
      ▲                      ┆
      │                      ┆
  SubTask Z ┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┘
```

The new Edge would form this cycle:

1. R is the Parent of T, so R can be `done` only when T is `done`
2. T is a Dependent of X, so T can be `done` only when X is `done`
3. X is the Parent of Z, so X can be `done` only when Z is `done`
4. Z would be a Dependent of R, so Z could be `done` only when R is `done`

So Nodoplex refuses to create that Dependency Edge.

In the same way, Task T has Requirement R as its Parent, and T depends on Feature F. A client tries to create a Hierarchical Edge from R to F:

```
  Feature F  <┄┄┄┄┄┄┐
      ▲             ┆
      │ new         ┆
Requirement R       ┆
      ▲             ┆
      │             ┆
   Task T ┄┄┄┄┄┄┄┄┄┄┘
```

F would be the Parent of R, R is the Parent of T, and T is a Dependent of F. So Nodoplex refuses to create that Hierarchical Edge.

These logical steps define whether a new Edge would form this kind of cycle. An implementation can use other steps that give the same result:

1. Nodoplex starts at the Child of a new Hierarchical Edge, or at the Prerequisite of a new Dependency Edge.
2. Nodoplex goes to each Child and each Prerequisite of that Node. From each Node that it reaches, Nodoplex goes on in the same way.
3. If Nodoplex reaches the Parent of the new Hierarchical Edge, or the Dependent of the new Dependency Edge, the new Edge would form this kind of cycle, and Nodoplex refuses the write.

Before each write, the graph has no cycle of this kind. Only a write that creates an Edge can form one, and the cycle must include the new Edge. So these steps find every cycle of this kind that a write would form.

For example, for the Dependency Edge from Z to R, Nodoplex starts at R, which is the Prerequisite. Nodoplex goes to T (a Child of R), then to X (a Prerequisite of T), and then to Z (a Child of X). Z is the Dependent, so Nodoplex refuses the write.

## Watchers

Every Node also has a list of **watchers**, which holds the user IDs of the users who watch the Node. A Node can have any number of watchers. To watch a Node, a user sends a watch request, and Nodoplex adds the user ID of the request to the list. To unwatch the Node, the user sends an unwatch request, and Nodoplex removes that user ID from the list.

Nodoplex doesn't notify watchers. A user who watches a Node doesn't watch the descendants of that Node.

## The Changefeed

Each of these writes produces exactly one Change Entry, however many Nodes and Edges the write changes:

- creating a Node
- updating a Node so that at least one tracked Field changes value
- archiving or unarchiving a Node
- deleting a Node
- creating an Edge
- updating the priority of a Hierarchical Edge
- deleting an Edge

These writes produce no Change Entry:

- a write that changes only untracked Fields
- a watch or unwatch request

A Change Entry holds:

- the time at which the write took effect, which is when Nodoplex recorded the Change Entry
- the user ID of the request that made the write
- a list of changes, with one change for each Node or Edge that the write created, updated, or deleted

The list of changes also holds an update of each of these Nodes, even if the request doesn't name the Node:

- each Node whose derived status changes because of the write
- each Node that becomes archived or stops being archived because of the write
- the Tail Node of each Edge that the write creates, updates, or deletes

If the write creates or deletes such a Node, the list holds the creation or the deletion of the Node, not an update.

Each change holds:

- its kind, which is creation, update, or deletion
- the Node or Edge before the change, unless the change is a creation
- the Node or Edge after the change, unless the change is a deletion

For example:

- Creating a Task with 2 Parents produces one Change Entry with at least 3 changes: the creation of the Task, and the creation of each of its 2 Hierarchical Edges.
- An update of several tracked Fields of a Node produces one Change Entry with at least 1 change. The Change Entry holds exactly one change for that Node, even if several Fields changed.
- Deleting a Node produces one Change Entry, which holds at least the deletion of the Node and the deletion of each Edge that points from the Node.
- Creating, updating, or deleting an Edge produces one Change Entry, which holds at least the change to the Edge and an update of the Tail Node.
- If every other Task of a Requirement is `done`, setting the last Task of the Requirement to `done` produces one Change Entry with the update of the Task. If the derived status of the Requirement changes to `done`, the Change Entry also holds the update of the Requirement, and of each other Node whose derived status changes as a result.

Each Change Entry has a **Change ID**. In each Changefeed, Change IDs start at 1 and increase by 1 with each Change Entry, in the order in which the writes took effect.

The **latest Change ID** of a Changefeed is the highest Change ID that Nodoplex has given to a Change Entry in that Changefeed. Any client can read the latest Change ID.

The state of a Node consists of:

- its tracked Fields
- whether it carries the archived mark
- whether it's archived
- the Edges that point from it

The **version** of a Node is the Change ID of the newest Change Entry that changed the state of the Node. Each Node holds its version.

A Node in a change holds:

- its ID and its Type
- its version
- all of its Fields, including untracked Fields
- its watchers
- whether it carries the archived mark
- whether it's archived

The Node before the change holds these values as they were before the change, and the Node after the change holds them as they were after the change. The version of the Node after the change is the Change ID of the Change Entry.

A Node in a change doesn't hold its Edges.

Nodoplex records no Change Entry for a write that changes only untracked Fields, or for a watch or unwatch request. So the Node after one change to it can differ from the Node before the next change to it.

By default, Nodoplex limits the number and the total size of the Change Entries that it keeps:

- The server configuration holds a limit on the number of Change Entries, and a limit on their total size. The 2 limits apply to the Change Entries of all Products together.
- When the Change Entries reach either limit, Nodoplex deletes the oldest Change Entries
- The server configuration has a setting that turns these deletions off. When the deletions are off, Nodoplex keeps every Change Entry until it deletes the Changefeed of a deleted Product.

## Node versions

Nodoplex uses the version to prevent lost updates (i.e. 2 clients silently overwriting each other's changes). When a client updates, archives, unarchives, or deletes a Node, the client sends the version of the Node that the client last read. When a client creates, updates, or deletes an Edge, the client sends the version of the Tail Node that the client last read. If the current version of that Node is higher than the version that the client sends, Nodoplex refuses the write.

- When a write changes the derived status of a Node, that Node gets a new version.
- An update that names only untracked Fields doesn't have to send the version. If the client sends a version, Nodoplex checks the version in the same way. If the client sends no version, Nodoplex doesn't check for concurrent writes.
- A watch or unwatch request doesn't send the version of the Node, and doesn't change the version. So 2 users who watch a Node at the same time don't conflict.

## Read operations

Nodoplex provides these read operations:

- reads by ID, which return Nodes and Edges by the ID of a Node or an Edge
- a list operation, which returns the Nodes of one Product that match the filters of the request
- an operation that lists the Products

The base and extended specifications can define more read operations (e.g. reading a Node and all of its Edges in one request).

### Reads by ID

Nodoplex provides these reads by ID:

- read a Node by the Node's ID
- read an Edge by the Edge's ID
- read the Edges that point from a Node, by the Node's ID
- read the Edges that point to a Node, by the Node's ID
- read the Head Node of an Edge, by the Edge's ID
- read the Tail Node of an Edge, by the Edge's ID

Each read by ID accepts an `include archived` setting, which is yes or no. The default is no. If the setting is yes, Nodoplex includes archived Nodes, and the Edges that have an archived Node at either end.

### The list operation

Nodoplex provides a **list** operation, which returns the Nodes of a Product.

The names below describe each filter and each sort. The base and extended specifications define how each name is written in code. A list request can give these filters:

- `Type`: the Nodes of the given Types
- `status`: the Nodes with the given statuses
- `assignee`: the Nodes assigned to the given user IDs, or the Nodes with no assignee
- `watcher`: the Nodes that the given user IDs watch, or the Nodes with no watchers
- `archived`: the archived Nodes, or the Nodes that aren't archived
- `children of`: the Children of the given Nodes
- `descendants of`: the descendants of the given Nodes
- `parents of`: the Parents of the given Nodes
- `ancestors of`: the ancestors of the given Nodes
- `dependents of`: the Nodes that directly depend on the given Nodes
- `prerequisites of`: the Nodes that the given Nodes directly depend on
- `blocked`: the blocked Nodes, or the Nodes that aren't blocked

When a list request gives a filter, the filter has one or more values (e.g. at least one Node ID in a `children of` filter). A Node matches a filter if the Node matches at least one of those values. For example, a `status` filter with the values `not_started` and `in_progress` matches every Node that is `not_started` or `in_progress`.

A Node matches a list request only if the Node matches every filter that the request gives. For example, a list request for available work can ask for the Tasks that are `not_started`, aren't archived, have no assignee, and aren't blocked.

If a list request leaves out a filter, Nodoplex uses the default of that filter. The default of `archived` matches only the Nodes that aren't archived. The default of each other filter matches every Node. For example, without a `Type` filter, Nodoplex returns Nodes of every Type.

A list request can give one of these sorts:

- `last updated`: the version of each Node, and then the ID of the Node. By default, Nodoplex sorts the highest version first, so the Node that changed most recently comes first. A list request can ask for the lowest version first instead. If the request gives no sort, Nodoplex uses this sort, highest version first. In both directions, Nodoplex sorts Nodes with the same version in ascending order of ID.
- `priority`: the priority number of the Hierarchical Edge from each Child to the Node that the `children of` filter gives, and then the ID of the Child. Nodoplex sorts the lowest priority number first, and sorts Children with the same priority number in ascending order of ID. Unless the `children of` filter gives exactly one Node, Nodoplex refuses a list request with this sort.

These logical steps define the result of a list request. An implementation can use other steps that give the same result:

1. Nodoplex filters the Nodes, and then sorts the Nodes that match
2. If the request gives a `start after` value, Nodoplex discards every Node that comes at or before that value in the sorted order. For the `last updated` sort, the `start after` value is the version and the ID of a Node. For the `priority` sort, the `start after` value is the priority number and the ID of a Node.
3. If the request gives a limit, Nodoplex returns at most that many Nodes. Otherwise, Nodoplex uses a default limit, which the base or extended specification defines.

So Nodoplex returns the Nodes one page at a time, with keyset pagination: each page starts after the last Node of the previous page, not at a numbered position. A request for the next page is the same request, with a `start after` value from the last Node of the previous page. If Nodoplex returns fewer Nodes than the limit, the page is the last page.

A Node that doesn't change stays in the same place in the sorted order, so exactly one page holds it. A Node that changes between 2 requests can move in the sorted order. When a Node moves:

- If the Node moves to a place after the last Node of the previous page, a later page holds the Node
- If the Node moves to a place before the last Node of the previous page, no later page holds the Node's new state

With the `last updated` sort, a Node that changes gets a version higher than every other version. So the `last updated` sort has these known limitations:

- With the highest version first, a listing can miss a Node that changes between 2 requests.
- With the lowest version first, a listing returns every Node that exists for the whole listing, but can return a Node twice if the Node changes between 2 requests.

### Listing the Products

The operation that lists the Products names no Product. The operation accepts the `archived` filter of the list operation, with the same default.

## How a client reads the Changefeed

A client reads the Changefeed in one of these ways:

- **poll**: the client sends a start Change ID and a limit. Nodoplex returns at most that many Change Entries whose Change ID is higher than the start Change ID. Nodoplex also returns the latest Change ID, even when the response holds no Change Entries. The latest Change ID in a response is the latest Change ID at the moment that Nodoplex read the Change Entries in that response.
- **subscribe**: the client opens a subscription, and can send a start Change ID. Nodoplex first pushes every existing Change Entry whose Change ID is higher than the start Change ID. Nodoplex then pushes each new Change Entry when Nodoplex records it. If the client sends no start Change ID, the subscription starts from the latest Change ID, so the client gets only new Change Entries.

With both poll and subscribe, Nodoplex sends Change Entries in ascending order of Change ID. The start Change ID is exclusive, so Nodoplex never sends the Change Entry that has the start Change ID itself.

**Expose Changefeed** is a setting in the server configuration, and is on by default. A client can't change the server configuration. While Expose Changefeed is on, a client can read the Changefeed. Each version of a Node is a Change ID, so Nodoplex records Change Entries even when Expose Changefeed is off. When Expose Changefeed is off, Nodoplex refuses poll and subscribe requests, but clients can still:

- read Nodes and Edges
- send list requests
- read the latest Change ID

### How a client filters the Changefeed

On both a poll and a subscription, a client can give a list of **filter groups**. Each filter group is a list of one or more filters. A change matches a filter group if the change matches every filter in the group. A Change Entry matches the list of filter groups if at least one change in the Change Entry matches at least one of the filter groups. Nodoplex sends each Change Entry that matches the list of filter groups, and no other Change Entry.

On a poll, Nodoplex applies the filters first, and then the limit. So only the Change Entries that are left after the filters count toward the limit.

Each filter has a name and one or more values. On the Changefeed, Nodoplex accepts only the list filters below, with the same names and values that they have on a list request. The other list filters aren't available on the Changefeed. Some of these filters match changes to Nodes and to Edges, and others match only changes to Nodes. No filter matches only changes to Edges.

These filters match a change to a Node or to an Edge:

- `Type`, which matches a change to a Node of one of the given Types, or to an Edge that has such a Node at either end
- `watcher`, which matches a change to a Node that the given users watch, or to an Edge that has such a Node at either end. Nodoplex checks the watchers that the Node has when Nodoplex sends the Change Entry. If a client has deleted the Node before then, Nodoplex checks the watchers that the Node had when the client deleted it.
- `archived`, which has the value "archived" or "not archived":
  - It matches a change to a Node if the Node is in that state.
  - It matches a change to an Edge if at least one of the 2 Nodes at the ends of the Edge is in that state.

These filters match only a change to a Node:

- `status`
- `assignee`

A `status`, `assignee`, or `archived` filter also gives which Node Nodoplex checks. This setting has no default. It has one of these values:

- `before`: Nodoplex checks the Node before the change.
- `after`: Nodoplex checks the Node after the change.
- `both`: Nodoplex checks the Node before the change and the Node after the change. The change matches only if both Nodes match.
- `either`: Nodoplex checks the Node before the change and the Node after the change. The change matches if at least one of the 2 Nodes matches.

For example, a `status` filter that holds `done` and `after` matches every change after which the Node is `done`. It matches a change that made the Node `done`. It also matches a change to another Field of a Node that was already `done`.

With `both` or `either`, a filter can give one value for the Node before the change and a different value for the Node after the change. For example, a `status` filter can hold these values:

- `both`
- `done`, for the Node before the change
- `in_progress`, for the Node after the change

That filter matches a change in which a Node moved from `done` to `in_progress`.

A creation of a Node has nothing before the change, and a deletion of a Node has nothing after the change. So:

- A creation of a Node never matches `before` or `both`. With `either`, Nodoplex checks only after the change.
- A deletion of a Node never matches `after` or `both`. With `either`, Nodoplex checks only before the change.

For a change to an Edge, the `archived` filter applies the setting to each of the 2 Nodes at the ends of the Edge. This includes the creation and the deletion of an Edge. The change matches if at least one of the 2 Nodes matches. For example:

- With `both`, the change matches if at least one of the 2 Nodes is in the given state before the change and after the change.
- With `after`, the deletion of an Edge matches if at least one of the 2 Nodes is in the given state after the deletion.

If one of the 2 Nodes doesn't exist at a time that the setting gives, Nodoplex doesn't check that Node at that time. For example, a write that deletes a Node also deletes the Edges that point from that Node. For each of those Edges, Nodoplex checks the deleted Node only before the change.

Each filter group has a default `archived` filter, which holds "not archived" and `either`. So by default, a change matches only if at least one Node that Nodoplex checks isn't archived. If a filter group holds its own `archived` filter, Nodoplex uses that filter instead of the default. If the client gives no filter groups, Nodoplex uses one filter group that holds only the default `archived` filter.

For example, a client can give these filter groups:

- a filter group with `Type` set to `Task`, and `assignee` set to the client's user ID and `after`
- a filter group with `Type` set to `Requirement`, and `assignee` set to no assignee and `after`

Each of these filter groups also holds the default `archived` filter. So these filter groups match every Change Entry that holds one of these changes, if the change also matches the default `archived` filter:

- a change after which a Task is assigned to the client's user
- a change after which a Requirement has no assignee

If a Change Entry matches, Nodoplex sends the whole Change Entry, including the changes in it that don't match.

Watching a Node affects only the `watcher` filter of a list request and the `watcher` filter of the Changefeed. Apart from these 2 filters, Nodoplex treats a watcher and a user who isn't a watcher the same way.

### How a client continues where it stopped

For each Changefeed that a client reads, the client stores a Change ID. The next time the client reads that Changefeed, the client sends that Change ID as the start Change ID. On both a poll and a subscription, the client stores the Change ID of a Change Entry only after the client has processed that Change Entry.

- If a client sends 0 as the start Change ID, Nodoplex starts from the oldest Change Entry that it still keeps. After Nodoplex has deleted Change Entries, the Change Entries that are left don't hold the whole history, so a client can't rebuild the graph from them.
- If a client sends a start Change ID higher than 0, and Nodoplex has already deleted a Change Entry whose Change ID is higher than the start Change ID, Nodoplex refuses the request with an error.
- If a poll returns fewer Change Entries than the limit, then after the client has processed all of them, the client stores the latest Change ID from the response. The next poll then starts after the Change Entries that didn't match the filters, so Nodoplex doesn't check those Change Entries against the filters again.

A subscription doesn't send the latest Change ID. So while the filter groups of a subscription match no new Change Entry, the stored Change ID of the client stays the same, and falls further behind the latest Change ID. If Nodoplex then deletes a Change Entry whose Change ID is higher than the stored Change ID, Nodoplex refuses the next subscribe request from that client, even though the client missed no Change Entry that matches. This is an edge case and a known limitation.

For a Node that still exists, Nodoplex checks the `watcher` filter against the watchers that the Node has when Nodoplex sends the Change Entry. So if a user starts or stops watching a Node between 2 reads of the same Change Entries, the 2 reads can give different results. For example, a client reads the Changefeed with a `watcher` filter that holds the user ID of its user:

- After the user starts to watch a Node, the client gets the new Change Entries about that Node. Because the stored Change ID of the client is already higher than the Change IDs of the older Change Entries about that Node, the client doesn't get those Change Entries. But a poll that starts from 0 returns the older Change Entries about that Node too.
- After the user unwatches a Node, a later read doesn't return the Change Entries about that Node, including those from the time when the user watched the Node.
