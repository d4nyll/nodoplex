# Mental Model

> This document is a high-level mental model of Nodoplex. The [base specification](01_base-specification.md) and the [extended specification](02_extended-specification.md) give the details (e.g. the full set of rules, the special cases, and the errors).

Nodoplex is a server that tracks the work on a product. It stores:

- the work that is planned
- the progress of that work
- the record of the work after it is done

Nodoplex stores the work as nodes, connected by edges. The nodes and the edges form a directed acyclic graph (DAG). A client can read and write the nodes and the edges. Nodoplex records each change in a changefeed. A client can subscribe to the changefeed, and then Nodoplex sends the client each change entry.

## Namespaces

- Every node and every edge is in exactly one **namespace**. A namespace is a name that a client can choose, and is opaque to Nodoplex
- A client must name a namespace in each request about nodes, edges, or the changefeed. Nodoplex reads and writes only the data in that namespace
- Nodoplex answers a request about a node in another namespace as if the node does not exist
- A namespace with no nodes is empty. A client can read an empty namespace. A client can use any namespace that meets the rules of the base specification

## Nodes

- A **node** is one item of the work on a product
- Each node has:
  - an **ID** (server-generated)
  - a **type**, which is immutable
  - fields that describe the work, which vary with the type
- These are the types:
  - **Vision**: a purpose of the product
  - **Goal**: an outcome that the product must achieve
  - **Feature**: a capability of the product
  - **Requirement**: a condition that a Feature must meet
  - **Task**: a unit of work that must be done to fulfil a Requirement
  - **Output**: a result of the work on a node of any other type (e.g. a Git branch, a Slack message)
- The types other than Output form this hierarchy, from the top:

  ```
  Vision -> Goal -> Feature -> Requirement -> Task
  ```

- An Output is below each of those types

- Every node except an Output must have a title. An Output can have a title. Every node can have a description
- Some types can have more fields:
  - Goal: a target date
  - Feature: a priority. A lower number means a higher priority
  - Requirement: acceptance criteria
  - Task: a **status**, which every Task must have, and which records the progress of the work on the Task. The status is one of these:
    - backlog
    - in progress
    - done
    - blocked
  - Output: an **output type**, which every Output must have, and a **payload**
- The output type is a name that a client can choose (e.g. `git`, `slack`). The payload is opaque to Nodoplex. Nodoplex stores the payload and returns it unchanged

## Edges

- An **edge** connects two nodes in the same namespace. Each edge is one of these kinds:
  - a **hierarchical edge**, which connects a node to one of its **parents**. A parent has the type directly above the type of its child in the hierarchy (e.g. Vision, for a Goal). A parent of an Output can have any type other than Output (e.g. Goal, for a change to a README file)
  - a **dependency edge**, which connects a node to a node of the same type that it **depends on**. The two nodes can have different parents. For example, a Task can depend on a Task of another Requirement, and a Vision can depend on another Vision
- Two nodes have at most one edge between them
- A node can have more than one parent (e.g. a Goal with two Vision nodes as parents). A Vision has no parent. Every node of any other type has at least one parent. When a client creates such a node, the client must give at least one parent. Nodoplex creates a hierarchical edge to each parent that the client gives
- Nodoplex refuses a request to delete the edge that connects a node to its only parent
- When a client deletes a node, Nodoplex also deletes:
  - each edge of the node, in either direction (e.g. a dependency edge from another node to the node)
  - each descendant that loses all of its parents, with the edges of that descendant. The deletion goes through the descendants layer by layer, from the top
- For example, node C is a child of both node A and node B. If a client deletes node B, node C still has the parent A. Thus Nodoplex keeps node C and each descendant of node C

## The changefeed

- Each of these changes produces one **change entry**:
  - the creation of a node
  - an update that changes at least one field of a node
  - the deletion of a node
  - the creation of an edge
  - the deletion of an edge
- Each change entry holds the kind of change (creation, update, or deletion) and the time of the change
- A change entry of a node also holds the node before and after the change. A change entry of the creation of a node has no node before the change. A change entry of the deletion of a node has no node after the change
- A change entry of an edge also holds the edge
- When a client deletes a node, Nodoplex produces one change entry for each node and each edge that it deletes
- Each change entry has a **change ID**. In each namespace, change IDs start at 1 and increase by 1, in the order in which the changes occur
- Each node holds the change ID of its latest change entry. This change ID identifies one version of the node. A change to an edge of the node leaves this change ID as it was
- When a client updates a node, the client must give the change ID of the version of the node that it last read. If the node has another change ID, Nodoplex refuses the update. Thus a retry of an update that changed the node gets a refusal, and no update overwrites a change that the client did not read
- The **changefeed** of a namespace is the sequence of all its change entries. Nodoplex keeps every change entry

## How a client reads the changefeed

- A client can read the changefeed in one of these ways:
  - **poll**: the client asks for the change entries after a change ID
  - **subscribe**: the client opens a subscription. Nodoplex sends the client each change entry after a **start change ID**. Then, each time a change occurs, Nodoplex sends its change entry
- If the client gives a change ID when it subscribes, the start change ID is that change ID. Otherwise, the start change ID is the latest change ID of the namespace: its highest change ID, or 0 if it has no change entry
- When a client opens a subscription, Nodoplex first sends the start change ID, with no change entry. Then Nodoplex sends the change entries
- In both ways, the client gets the change entries in ascending order of change ID
- If a client names node types, the client gets only these change entries:
  - the change entries of the nodes of those types
  - the change entries of the edges that connect at least one node of those types
- For each namespace, a client can keep the highest change ID that it received, including the start change ID. After a subscription closes, the client can read the changefeed after that change ID, and get every later change entry
- A client that reads the changefeed after change ID 0 gets every change entry. These change entries include the creation of every node and every edge. If a client lists the nodes and then subscribes with no change ID, the client misses each change that occurs between the two requests
