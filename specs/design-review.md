# Design review

This review lists each inconsistency, unintuitive behavior, and gap that the reviewer found in Nodoplex at commit [`72c93ef`](https://github.com/d4nyll/nodoplex/tree/72c93ef3fe2e271585fdbf3858b81feb184ae329). That commit was the head of `main` on 2026-11-08.

For each item, this review gives the status quo, and the change that the [mental model](00_mental-model.md), the [base specification](01_base-specification.md), and the [extended specification](02_extended-specification.md) describe. Below, "the documents" means those three documents. A citation such as "base: P2" names a rule, an operation, a step, or a section of the base specification. A citation such as "extended: X1" names one of the extended specification. Some items also give a decision that sets the change.

If an "Observed" line names no other setup, the line reports a result from the stack at `72c93ef`. Docker Compose ran that stack on 2026-11-08, with Neo4j 2026.10.1 and PostgreSQL 18. Links to Nodoplex code pin commit `72c93ef`. Links to the web client pin `nodoplex-web` commit [`d269754`](https://github.com/d4nyll/nodoplex-web/tree/d269754ec643ed456f986f61d1bac1167aff89fc), the head of its `main` on the same day.

The Ergos mental model is `specs/00_mental-model.md` in the ergos repository. Links to the Ergos mental model pin commit [`ba18a2d`](https://github.com/d4nyll/ergos/commit/ba18a2df4570ce18d3119c953496ef65e19f8988) of the branch `docs/ergos-mental-model`.

## The main changes

- Each edge is its own item, with a source, a target, and one of these types (R5):
  - `hierarchical`, which replaces `BREAKS_DOWN_TO` and `IMPLEMENTED_BY`
  - `dependency`, which replaces `DEPENDS_ON`
- A namespace holds each edge at most one time (R1)
- Like the code at `72c93ef`, the documents let a node have more than one parent. `GetAncestors` and `GetDescendants` replace `GetLineage` and `GetSubtree`. Each returns each node one time, with the hierarchical edges between the nodes (R2, R3)
- Every node except a Vision has at least one parent (R4)
- A parent of an Output can have any type other than Output (R5)
- When a client deletes a node, the server also deletes its edges, and each descendant whose parents are all deleted (R6)
- Each change to a node or to an edge has its own change entry (R6, R21)
- The change IDs of each namespace start at 1 and increase by 1, in the order in which the changes take effect. The changes of an operation and their change entries are atomic (R22 to R25)
- `Subscribe` starts after a change ID, and sends each later change that matches the request, in order, one time each (R28)
- Every error response carries a JSON `Error` with a `code`, except in the cases that X1 and X4 exempt (extended: X1, X4). A server fault is an `internal_error` (R30 to R32)
- `UpdateNode` needs the `changeId` that the client last read. If the node has another `changeId`, `UpdateNode` refuses the update (R13)

## Graph structure

### R1. The server stores the same parent edge more than one time

- Status quo: `CreateEdge` writes a `BREAKS_DOWN_TO` or `IMPLEMENTED_BY` edge with `CREATE`, and with no check for an identical edge ([repository.go:202-205](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/repository.go#L202-L205)). A request for a second, identical `DEPENDS_ON` edge has no effect ([repository.go:281-298](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/repository.go#L281-L298)). Thus the server handles a duplicate parent edge and a duplicate dependency edge in different ways
- Observed: two identical `POST .../edges` requests for Vision V1 to Goal G1 both returned 201. `GET .../nodes/<V1>/children` then listed G1 two times. One `DELETE .../edges` removed both edges
- In the documents: a namespace must not hold two edges with the same type, source, and target. For an edge that exists, `CreateEdge` returns the edge, and records no change (base: G1, `CreateEdge` step 5)

### R2. `GetLineage` and `GetSubtree` list a node one time for each path to it

- Status quo: the server stores each parent edge as a `BREAKS_DOWN_TO` or `IMPLEMENTED_BY` relationship from the parent to the child. It accepts any number of parents for a node. It checks that each parent has the type directly above the type of the child ([service.go:91-131](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/service.go#L91-L131), [models.go:106-115](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/models.go#L106-L115)). `GetNode` returns the node with a `parents` array and a `children` array ([handlers.go:153-185](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L153-L185))
- Status quo: `GetLineage` returns every path from every root (a node with no parent), one after the other ([repository.go:468-480](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/repository.go#L468-L480)). `GetSubtree` puts a copy of a node under its parent for each path to the node ([repository.go:408-444](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/repository.go#L408-L444)). `api/openapi.yaml` describes one path: "Trace ancestors up to the root Vision" and "Ordered list of ancestor nodes from root to this node" ([openapi.yaml:263-267](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/api/openapi.yaml#L263-L267))
- Observed: Goal G1 had the parents V1 (by two edges) and V2. Its lineage was `[V1, G1, V1, G1, V2, G1]`. In another namespace, Task T had two parents, the Requirements Q1 and Q2. The subtree of their Feature showed T, with its Output child, under both Q1 and Q2. A request for a Vision as the parent of a Feature got 400
- In the documents: each edge is its own item, with a type, a source, and a target (mental model: Edges; base: Edge schema). A parent of the wrong type gets `invalid_reference` (base: P1, `CreateNode` step 4, `CreateEdge` step 3)
- In the documents: `GetAncestors` replaces `GetLineage`, and `GetDescendants` replaces `GetSubtree`. Each returns each node one time, in id order. It also returns each hierarchical edge that connects two of those nodes, or one of them and the node of the request (base: `GetAncestors`, `GetDescendants`). From these edges, a client can rebuild each path
- In the documents: `GetNode` returns the node alone. `GetParents` and `GetChildren` return its parents and its children

### R3. `GetLineage` returns an empty list for a root, and stops at a root of any type

- Status quo: the query matches only paths of at least one edge ([repository.go:469](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/repository.go#L469)). Thus the lineage of a root is empty, but the lineage of every other node ends with the node itself. The lineage starts at a node that has no parent, of any type ([repository.go:472](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/repository.go#L472)). `api/openapi.yaml` describes a lineage that starts at the root Vision
- Observed: `GET .../nodes/<V1>/lineage` for the root Vision V1 returned `{"nodes":[]}`. For a Task whose ancestors included a Feature with no parent, the lineage started at the Feature
- In the documents: the ancestors of a node never include the node itself (base: phrase "ancestors", `GetAncestors`). A Vision has no parent, so it has no ancestors. Each node except a Vision has at least one parent (base: P2). Thus the ancestors of each such node include at least one Vision

### R4. A client needs two requests to create a node under a parent

- Status quo: `POST .../nodes` creates a node with no parent, and `POST .../edges` links it. Between the two requests, the node is a root. If the client stops after the first request, the node stays a root. The web client creates every node of a sequence of new nodes first, then every edge ([CreateNodeDialog.tsx:166-196](https://github.com/d4nyll/nodoplex-web/blob/d269754ec643ed456f986f61d1bac1167aff89fc/src/components/NodeForm/CreateNodeDialog.tsx#L166-L196))
- Status quo: a client moves a node to another parent with a `DELETE` and a `POST`. If the `DELETE` runs first, the node has no parent between the two requests. If the `POST` runs first, the node has two parents
- Decision: each node except a Vision must have at least one parent
- In the documents: `CreateNode` takes `parentIds`, and creates the node and its hierarchical edges in one operation (base: `CreateNode` step 5). It refuses a request that gives no parent for a node other than a Vision (base: P2, the checks before step 1, `CreateNode` step 3)
- In the documents: `DeleteEdge` refuses to delete the last hierarchical edge of a node (base: `DeleteEdge` step 2). Thus, to move a node to another parent, a client creates the new hierarchical edge first, and then deletes the old one. `DeleteNode` deletes each descendant whose parents are all deleted, so no node that remains loses its last parent (base: `DeleteNode` step 2)

### R5. Two edge types have the same meaning

- Status quo: `BREAKS_DOWN_TO` and `IMPLEMENTED_BY` both mean "is the parent of". Which one applies depends on the type of the child ([models.go:106-115](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/models.go#L106-L115)). `IMPLEMENTED_BY` connects a Task to an Output. An Output can describe a Slack message, which does not implement a Task. `DELETE .../edges` accepts a JSON body ([openapi.yaml:179-194](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/api/openapi.yaml#L179-L194)), and RFC 9110 gives a body in a `DELETE` request no defined meaning
- Decision: an Output records a result of the work on a node of any type other than Output (e.g. a change to a README file, for a Goal)
- In the documents: a parent of an Output can have any type other than Output (base: Node schema, P1)
- In the documents: one edge type, `hierarchical`, replaces `BREAKS_DOWN_TO` and `IMPLEMENTED_BY` (base: Edge schema). `DeleteEdge` takes the edge in its path, with no body (extended: Operations)

### R6. `DeleteNode` removes edges and records nothing about them

- Status quo: `DETACH DELETE` removes every edge of the node ([repository.go:186-188](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/repository.go#L186-L188)). Each child that has no other parent edge becomes a root. Each node that depended on the deleted node loses that dependency. The server records no change entry for either change
- Observed: Requirement RB depended on Requirement RA. After `DELETE .../nodes/<RA>`, the dependencies of RB were `[]`. The `requirement` change entries were `created`, `created`, and `deleted`, and no entry recorded the change to RB
- In the documents: `DeleteNode` deletes each edge of the node, including each dependency edge from another node to it. It also deletes each descendant whose parents are all deleted, one level at a time, with the edges of that descendant (base: `DeleteNode`)
- In the documents: `DeleteNode` records a `deleted` change entry for each edge and for each node that it deletes (base: `DeleteNode` step 2)
- Cost: the web client asks for a confirmation ([DetailPanel.tsx:282-294](https://github.com/d4nyll/nodoplex-web/blob/d269754ec643ed456f986f61d1bac1167aff89fc/src/components/Layout/DetailPanel.tsx#L282-L294)), and then deletes a node with one request ([DetailPanel.tsx:201-212](https://github.com/d4nyll/nodoplex-web/blob/d269754ec643ed456f986f61d1bac1167aff89fc/src/components/Layout/DetailPanel.tsx#L201-L212)). With the documents, that request can also delete descendants, so the confirmation must say so

## Dependencies

### R7. The server answers a self-dependency and a cycle with different HTTP statuses

- Status quo: the server accepts a dependency between two nodes of the same type in a namespace, whatever their parents ([models.go:116-123](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/models.go#L116-L123), [repository.go:281-306](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/repository.go#L281-L306)). When requests run concurrently, a lock for each namespace keeps the cycle check correct ([repository.go:256-279](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/repository.go#L256-L279))
- Status quo: a request that makes a node depend on itself gets 400 ([service.go:97-99](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/service.go#L97-L99)). A request that makes a longer cycle gets 409 ([handlers.go:266-269](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L266-L269)). A self-dependency is also a cycle. A request for a dependency between nodes of two different types also gets 400 ([service.go:111-125](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/service.go#L111-L125), [handlers.go:270](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L270))
- Observed: a request that made a Vision depend on a Vision got 201. So did a request that made a Task depend on a Task with a different parent. A request that made a Goal depend on a Feature got 400. In each of 50 trials, two concurrent requests asked for A to depend on B, and for B to depend on A. Each time, one request got 201 and the other got 409
- In the documents: a self-dependency and a cycle both get `dependency_cycle` (base: D2, `CreateEdge` step 6). A dependency on a node of another type gets `invalid_reference` (base: D1, `CreateEdge` step 4). The extended specification gives both codes the status 409 (extended: X2)

## Node fields

### R8. The server stores nodes without their required fields

- Status quo: of the required fields of a `CreateNode` request body, the server checks only `type` ([convert.go:15-18](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/convert.go#L15-L18), [convert.go:95-96](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/convert.go#L95-L96)) and the `outputType` of an Output ([convert.go:102-112](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/convert.go#L102-L112)). `parseCreateNodeRequest` copies the zero value of each other required field that is not in the request (e.g. [convert.go:28](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/convert.go#L28), [convert.go:81](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/convert.go#L81))
- Observed: `{"type":"vision"}` returned 201 with `"title":""`. A request for a Task with no `status` got 201 with `"status":""`, which is not a `TaskStatus`. A request for an Output with no `outputType` got 400 `outputType must not be empty`
- In the documents: check 3 before step 1 refuses a request that omits a required field (base: Operations). The values of `title` and `outputType` must have at least 1 character

### R9. The server accepts any string for an enum

- Status quo: when a client creates or updates a node, Task `status` takes any string ([convert.go:81](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/convert.go#L81), [convert.go:138-140](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/convert.go#L138-L140)). `Subscribe` does not check its `type` values ([handlers.go:454-460](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L454-L460))
- Observed: a request that created a Task with `"status":"bogus"` got 201, and a later `GET` returned `"status":"bogus"`. `GET .../subscribe?type=bogus` returned 200, and sent no event while 2 changes occurred in the namespace
- In the documents: check 2 before step 1 refuses a value outside its schema type, in the query and in the body (base: Operations). The schema type of `status` is `TaskStatus`, so check 2 refuses `"status":"bogus"`

### R10. The server stores fields that the type of the node does not have

- Status quo: `UpdateNodeRequest` accepts every field for every type ([openapi.yaml:779-805](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/api/openapi.yaml#L779-L805)), and `UpdateNode` stores what it gets ([convert.go:124-148](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/convert.go#L124-L148), [repository.go:158-160](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/repository.go#L158-L160)). Node responses omit a field that the type of the node does not have ([convert.go:151-246](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/convert.go#L151-L246)). Change entries show such a field ([handlers.go:629-659](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L629-L659)). The `search` and `status` filters also match it ([repository.go:91-98](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/repository.go#L91-L98)). When a client creates a node, the server drops a field that the type does not have, with no error
- Observed:
  - `PATCH {"status":"done"}` on a Feature returned 200 with no `status`. Then `GET .../nodes?status=done` listed the Feature, and its `updated` entry carried `"status":"done"`
  - `PATCH {"outputType":"git","payload":{"secret":"x"}}` on a Task returned 200 with neither field. Its `updated` entry carried both
  - `PATCH {"title":"Secret Title"}` on an Output returned no `title`, but `?search=Secret` found the Output
  - `POST` of a Vision with `"priority":3` returned 201 with no `priority`
- In the documents: step 1 of `CreateNode` and step 2 of `UpdateNode` refuse a field that the type of the node does not have (`invalid_request`)

### R11. A client cannot remove an optional field

- Status quo: `updateRequestToProps` skips every `null` ([convert.go:124-147](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/convert.go#L124-L147)). After a client sets a description, priority, or target date, the client can change the value, but cannot remove it
- Observed: `PATCH {"description":null}` returned 400 `no fields to update`
- In the documents: in `UpdateNode`, a field with no value removes an optional field (base: `UpdateNode` step 5). In JSON, a field with no value is a member with the value `null` (extended: JSON). If a request changes no value, it succeeds, and the server appends no change entry

### R12. Nodoplex and Ergos both store the state of the work on a Task

- Status quo: in `api/openapi.yaml`, only Tasks have a `status` ([openapi.yaml:446-448](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/api/openapi.yaml#L446-L448), [openapi.yaml:536-551](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/api/openapi.yaml#L536-L551)). In Ergos, "[e]very node except an Output node has exactly one Lockstep stream, in which the work on that node is coordinated" ([00_mental-model.md:68](https://github.com/d4nyll/ergos/blob/ba18a2df4570ce18d3119c953496ef65e19f8988/specs/00_mental-model.md?plain=1#L68)). "A stream is live until it is terminated" ([00_mental-model.md:78](https://github.com/d4nyll/ergos/blob/ba18a2df4570ce18d3119c953496ef65e19f8988/specs/00_mental-model.md?plain=1#L78)). The `status` of a Task in Nodoplex can differ from the state of the stream of that Task in Ergos.
- Decision: Tasks keep `status`, so that a client can read the progress of a Task
- Decision: SubTasks have `status` too, because a SubTask is a unit of work like a Task
- In the documents: every Task and SubTask has a `status`, whose value is one of `backlog`, `in_progress`, `done`, and `blocked` (base: Node schema). If the request gives a `status`, `ListNodes` returns only the Tasks and SubTasks whose `status` has that value (base: `ListNodes` step 4)

### R13. A node has no version

- Status quo: a node carries no version. The Ergos mental model says: "Each change to a definition creates a new version" ([00_mental-model.md:63](https://github.com/d4nyll/ergos/blob/ba18a2df4570ce18d3119c953496ef65e19f8988/specs/00_mental-model.md?plain=1#L63)). With each contribution, Ergos records "the latest version of the definition of the node of the seat's stream that the occupant received before it contributed" ([00_mental-model.md:215-216](https://github.com/d4nyll/ergos/blob/ba18a2df4570ce18d3119c953496ef65e19f8988/specs/00_mental-model.md?plain=1#L215-L216)). `UpdateNode` applies each valid request, and appends a change entry each time ([handlers.go:204-220](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L204-L220)). Thus a client that retries an update after a lost response applies it a second time. The Ergos mental model counts only changes to the definition as versions
- Decision: each node carries `changeId`, and `UpdateNode` refuses an update whose `changeId` is not the current one
- In the documents: each node carries `changeId`, the ID of its latest change entry. The change entry with that ID holds the current version of the node. `UpdateNode` needs the `changeId` of the node that the client last read. If the node has another `changeId`, `UpdateNode` refuses with `change_id_mismatch` (base: `UpdateNode` step 4)
- In the documents: a retry of an update that changed the node gets `change_id_mismatch`. No update overwrites a change that the client did not read
- In the documents: each change to an edge has its own change entry, and leaves the `changeId` of each node as it was. A change to the `status` of a Task changes its `changeId`

### R14. The server rounds large numbers in an Output payload

- Status quo: the server decodes the payload of a request into a `map[string]interface{}`, the type that oapi-codegen generates from the `OutputPayload` schema ([openapi.yaml:581-587](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/api/openapi.yaml#L581-L587)). When the server reads a node, it decodes the stored payload into a `map[string]any` again ([models.go:139](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/models.go#L139), [models.go:213](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/models.go#L213)). Go's `encoding/json` decodes each JSON number in such a map to a `float64`. `api/openapi.yaml` says that Nodoplex "stores it and returns it unchanged" ([openapi.yaml:585](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/api/openapi.yaml#L585))
- Observed: `POST` of an Output with `"payload":{"n":9007199254740993}` returned 201 with `"payload":{"n":9007199254740992}`. A later `GET` returned the same rounded value
- In the documents: T1 requires the server to return each payload as the same value as the payload that set it (base: T1). Two JSON numbers are the same only if they are equal as exact decimal numbers (extended: JSON). If the server keeps the payload as raw JSON text (e.g. `json.RawMessage`), it meets T1

## Lists, search, and traversals

### R15. `ListNodes` returns pages in no fixed order

- Status quo: the query has no `ORDER BY` ([repository.go:125](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/repository.go#L125)), so consecutive pages can skip or repeat a node
- In the documents: these lists use id order:
  - `ListNodes`, `GetParents`, `GetChildren`, `GetAncestors`, `GetDescendants`, `GetDependencies`, and `GetDependents`
  - the edges of a `Subgraph`
- Gap that remains: if a client creates or deletes a node between two requests, pages that use `offset` can still skip or repeat a node. A client that must see every node can read the changefeed. Also, id order matches the order of creation only if the server makes time-ordered UUIDs (e.g. version 6 or 7 of RFC 9562). For that, the server also needs to put the UUIDs of one timestamp in the order of creation (RFC 9562 section 6.2). The documents leave the UUID version to the implementation

### R16. `ListNodes` can compute the value of `total` and the page from two states

- Status quo: the count and the page are two queries that do not share a transaction ([repository.go:110-129](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/repository.go#L110-L129)). A write between them changes one result but not the other. This claim comes from the code, not from a test on the stack
- In the documents: S5 requires a read operation to compute its whole output from one state (base: S5)

### R17. The server answers a failed read with 200 and an incomplete list

- Status quo: Neo4j refuses a negative `SKIP` or `LIMIT`. In the driver, `Next` returns `false` for an error, the same as at the end of the records. `ListNodes` never checks `Err()` ([repository.go:131-139](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/repository.go#L131-L139)). `collectNodes` ([repository.go:533-546](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/repository.go#L533-L546)) and `GetSubtree` ([repository.go:425-444](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/repository.go#L425-L444)) do the same. Thus each of these reads that fails part way returns an incomplete list with 200
- Observed: `GET .../nodes?limit=-1` and `GET .../nodes?offset=-1` each returned 200 `{"nodes":[],"total":9}`. On a stack at the earlier commit `ae1d2e1`, which uses the same driver version, a Go program sent the same query. It got no error from `Run`, and `false` from the first `Next`. Then `Err()` returned `LIMIT: Invalid input. '-1' is not a valid value. Must be a non-negative integer.`
- In the documents: check 2 before step 1 refuses a query value outside its range (`invalid_request`). E1 requires an `internal_error` for any other failure of the read (base: Operations, E1)

### R18. `ListChanges` returns 500 with a PostgreSQL message for a negative limit

- Status quo: `ListChanges` passes the value of `limit` to PostgreSQL unchanged, and returns the database error as a 500 ([handlers.go:409-418](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L409-L418))
- Observed: `GET .../changes?type=vision&limit=-1` returned 500 `{"message":"ERROR: LIMIT must not be negative (SQLSTATE 2201W)"}`
- In the documents: the value of `limit` must be from 1 to 1000, both inclusive. Check 2 before step 1 refuses other values (`invalid_request`)

### R19. The server ignores the maximum `limit` that `api/openapi.yaml` gives

- Status quo: `api/openapi.yaml` gives a maximum `limit` of 200 for nodes and 1000 for changes ([openapi.yaml:76](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/api/openapi.yaml#L76), [openapi.yaml:390](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/api/openapi.yaml#L390)). The handlers pass any value ([handlers.go:125-132](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L125-L132), [handlers.go:409-412](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L409-L412))
- Observed: in a namespace with 210 nodes, `GET .../nodes?limit=5000` returned all 210
- In the documents: check 2 before step 1 refuses a `limit` value outside its range (`invalid_request`)

### R20. `ListNodes` matches the value of `search` case-sensitively

- Status quo: `api/openapi.yaml` calls `search` a "Full-text search on title and description" ([openapi.yaml:63](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/api/openapi.yaml#L63)). The query is a case-sensitive `CONTAINS` ([repository.go:95-97](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/repository.go#L95-L97))
- Observed: `search=sso` did not find the Feature "SSO Integration", and `search=SSO` found it
- In the documents: `ListNodes` step 3 applies Unicode simple case folding to each string (base: `ListNodes`). Step 3 then keeps each node whose folded title or folded description holds the folded value of `search` as a substring

## Changefeed

### R21. The server omits edge changes from the changefeed

- Status quo: `CreateEdge` and `DeleteEdge` append no change entry and send no event ([handlers.go:247-309](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L247-L309), [README.md:201](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/README.md?plain=1#L201)). The changefeed does not show which edges changed
- In the documents: each creation and each deletion of an edge appends one change entry (base: `CreateEdge`, `DeleteEdge`). The value of `entity` of that change entry is `edge` (base: ChangeEntry schema). `CreateNode` records one change entry for the node, and one for each of its hierarchical edges. `DeleteNode` records one for each node and each edge that it deletes

### R22. The server numbers the changes of all namespaces from one sequence

- Status quo: `change_id` is one `BIGSERIAL` for the whole table ([migrations.go:14](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/changelog/migrations.go#L14), [README.md:183](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/README.md?plain=1#L183)). A client that reads one namespace gets change IDs with gaps. From the gaps, the client can estimate the number of changes in the other namespaces
- Observed: one namespace got the change IDs 34 and 36, and another namespace got 35
- In the documents: the server numbers the changes of each namespace from 1, with no gap (base: C1, C2)

### R23. A client that polls can skip a change that commits late

- Status quo: PostgreSQL gives a row its `BIGSERIAL` value at insert time, not at commit time. Thus a change with a higher ID can become visible before a change with a lower ID. After the higher ID becomes visible, a client can poll before the lower ID becomes visible. That client continues with a `since` value above the lower ID, and never gets that change. Each `Append` is one INSERT with an automatic commit ([store.go:35-47](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/changelog/store.go#L35-L47)). Thus the time between the insert and the commit is short, but a client can poll during that time
- Observed in a test table with a `BIGSERIAL` key: session A inserted ID 1, and waited 3 s before its commit. Session B inserted ID 2 and committed. A reader saw only ID 2. After session A committed, the reader saw IDs 1 and 2
- In the documents: S1 requires the server to run the writes of a namespace one at a time. C1 and C2 require the server to number the changes in that order (base: S1, C1, C2)

### R24. The server can write a node with no change entry

- Status quo: the handler writes the node to Neo4j. Then `publishChange` writes the change entry to PostgreSQL ([handlers.go:92-108](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L92-L108), [handlers.go:574-621](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L574-L621)). If the second write fails, the server logs the failure and answers 500, but keeps the node change ([handlers.go:598-608](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L598-L608), [handlers.go:623-627](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L623-L627))
- Observed: with PostgreSQL stopped, `POST .../nodes` for a Vision returned 500, with a message that starts with `the node change was saved but not recorded in the changelog:`. The node existed, and after PostgreSQL started again, the namespace had no `vision` change entry
- Decision: Neo4j keeps the nodes and the edges, and PostgreSQL keeps the change entries. Neo4j keeps only the change entries that the server has not copied to PostgreSQL yet
- In the documents: S2 requires the changes of an operation and their change entries to be atomic (base: S2). The server writes the changes, their change entries, and the latest change ID of the namespace in one Neo4j transaction. Then the server copies each change entry to PostgreSQL, in ascending order of `changeId`, and can delete it from Neo4j (extended: B1 to B5). This is the transactional outbox pattern
- In the documents: `ListChanges` and `Subscribe` read from PostgreSQL. Before each read, the server waits until PostgreSQL holds each change entry up to the latest change ID in Neo4j (extended: B6, B7). Thus a client that polls right after an operation gets the change entries of that operation, and a stream skips no change entry

### R25. When updates run concurrently, the server records a `previous` that differs from the `node` of the entry before it

- Status quo: `UpdateNode` and `DeleteNode` read the node in one transaction, and write it in another ([repository.go:147-160](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/repository.go#L147-L160), [repository.go:180-188](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/repository.go#L180-L188)). Updates to one node can run at the same time
- Observed: in each of 2 trials, a client sent 30 concurrent `PATCH` requests that set the title of one node. In each trial, 29 of the 30 `updated` entries had a `previous` that differed from the `node` of the entry before it. For example, the `previous` of change 281 held the title `start`, but change 280 set the title to `t2`. In both trials, the last entry matched the stored node
- In the documents: S1 requires the server to run the writes of a namespace one at a time (base: S1). Thus no other write can change a node while an update reads and changes it

### R26. The server gives one change two different timestamps

- Status quo: a live event gets the time from the clock of the Go process ([handlers.go:587](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L587)). The stored entry gets `NOW()` from PostgreSQL ([migrations.go:20](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/changelog/migrations.go#L20)). `/changes` and the replay of `Subscribe` send the stored time
- Observed: one change had `04:35:59.871670418Z` in the live event stream, and `04:35:59.87204Z` in `/changes` and in a replay
- In the documents: each change entry has one `timestamp`, and C3 forbids the server to modify the entry (base: ChangeEntry schema, C3)

### R27. `ListChanges` accepts exactly one type, and `Subscribe` accepts any number

- Status quo: `ListChanges` needs exactly one `type` ([openapi.yaml:373-378](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/api/openapi.yaml#L373-L378)). `Subscribe` accepts zero or more ([openapi.yaml:343-351](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/api/openapi.yaml#L343-L351)). A client that cannot send `Last-Event-ID` (e.g. a browser on its first connection, R28) must poll for the changes that it missed. To get the changes of all types, it must poll one time for each type, and then merge the lists
- Observed: `GET .../changes` with no `type` returned 400 `Query argument type is required, but not found`. With two `type` values, it returned 400 `Invalid format for parameter type: multiple values for single value parameter 'type'`
- In the documents: `ListNodes`, `ListChanges`, and `Subscribe` each take zero or more `type` values

### R28. The server can drop events from the event stream, and a browser cannot choose where the stream starts

- Status quo: `Subscribe` replays the change entries after the change ID in `Last-Event-ID`, and then sends live events ([handlers.go:508-531](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L508-L531)). No other input (e.g. a query parameter) sets the start position ([openapi.yaml:334-351](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/api/openapi.yaml#L334-L351)). A browser `EventSource` cannot set `Last-Event-ID` on its first connection. Thus a browser that polls `/changes` and then subscribes misses each change between the two requests
- Status quo: during the live stream, the event bus drops an event for a subscriber whose buffer of 64 events is full ([bus.go:70](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/events/bus.go#L70), [bus.go:99-104](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/events/bus.go#L99-L104)). Because of R22, change IDs already have gaps, so the subscriber cannot detect a dropped event. After the subscriber receives a later event, its last event ID is above the change ID of the dropped event. Thus a reconnect with that event ID does not replay the dropped event
- Observed: in a namespace that held change 37, `Last-Event-ID: 36` replayed change 37. `?since=0` sent no event in 2 s
- In the documents: `Subscribe` starts after the change ID in `Last-Event-ID` or `since`, or after the latest change (base: `Subscribe` step 1; extended: I5). `Subscribe` first sends the start change ID as an event ID (extended: F2). Then it sends each later change that matches the request, in order, one time each. When a browser reconnects, it sends the last event ID that it received, in the `Last-Event-ID` header
- Implementation note: the server can read the events of each `Subscribe` connection from the change entries. Then the event bus only signals that new entries exist, and a slow client loses no event. The signal needs no buffer of events: one pending signal for each connection is enough, because each signal makes the connection read every later entry. At `72c93ef`, the buffer of 64 events exists because the live events travel through the event bus, and only the replay at connection time reads the database

### R29. `nextSince` holds the same value as the `changeId` of the last entry

- Status quo: the value of `nextSince` is the `changeId` of the last entry in the list ([openapi.yaml:878-883](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/api/openapi.yaml#L878-L883), [handlers.go:444-448](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L444-L448))
- In the documents: `ChangeList` has only `changes`
- Cost: the activity feed of the web client reads `nextSince` ([ActivityFeed.tsx:56](https://github.com/d4nyll/nodoplex-web/blob/d269754ec643ed456f986f61d1bac1167aff89fc/src/components/ActivityFeed/ActivityFeed.tsx#L56))

## Errors

### R30. The server sends some error responses as plain text

- Status quo: `NewRouter` sets no `ErrorHandlerFunc` ([handlers.go:49](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L49)). Thus the code that oapi-codegen v2.8.2 generates answers a bad parameter with `http.Error`, as plain text. `http.ServeMux` answers an unknown path or method as plain text. `withRecovery` sends JSON with a plain text content type ([main.go:166](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/cmd/server/main.go#L166)). `api/openapi.yaml` gives every 400 and 404 an `Error` JSON body
- Observed: `?limit=abc`, `GET .../nodes/not-a-uuid`, and the header `Last-Event-ID: abc` each got 400 `text/plain`. An unknown path got 404 `text/plain` `404 page not found`. `PUT .../nodes` got 405 `text/plain` `Method Not Allowed`
- In the documents: X1 requires an `Error` body on each error response to a request that is not `HEAD` (extended: X1). These checks before step 1 cover the cases above (extended: How the server checks a request; base: Operations):
  - a bad query parameter or header
  - an unknown path
  - an unknown method
  - a path `id` that is not a uuid
- In the documents: E1 and J2 make a server fault an `internal_error` with the content type `application/json` (base: E1; extended: J2)

### R31. The server answers some server faults with 400 or 200, and its error messages show internal details

- Status quo: `CreateNode`, `ListNodes`, `UpdateNode`, and `CreateEdge` answer most errors with 400 and the text of the error. The exceptions are "not found" and a dependency cycle ([handlers.go:93-96](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L93-L96), [handlers.go:134-138](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L134-L138), [handlers.go:210](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L210), [handlers.go:270](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L270)). Most other handlers answer with 500 and the text of the error (e.g. [handlers.go:160](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L160), [handlers.go:416](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L416), [handlers.go:626](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L626)). If a read of the change entries to replay fails before `Subscribe` sends an event, `Subscribe` answers 200 with an empty body ([handlers.go:513-527](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/api/handlers.go#L513-L527))
- Observed: with Neo4j stopped, `POST .../nodes` returned 400 `create node: ConnectivityError: dial tcp: lookup neo4j on 127.0.0.11:53: no such host`. `GET .../nodes/{id}` returned 500 with the same kind of text. With PostgreSQL stopped, `GET .../changes` and `POST .../nodes` each returned 500 with the database user, the database name, and the DNS error. In that state, `GET .../subscribe` with `Last-Event-ID: 0` returned 200 `text/event-stream` with an empty body
- In the documents: E1 requires an `internal_error` for a server fault, and E2 lists what its `message` must not contain (base: E1, E2). The extended specification gives `internal_error` the status 500 (extended: X2)
- In the documents: `Subscribe` reads the latest change ID before it opens the stream (base: `Subscribe` steps 1 to 3). Thus a failure of that read gets `internal_error`. After the stream opens, a failure ends the stream (extended: X3, F6)

### R32. An error carries only a message

- Status quo: `Error` has only `message` ([openapi.yaml:459-464](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/api/openapi.yaml#L459-L464)). To distinguish one 400 from another, a client must read the text. For example, only the text shows whether the edge type is unknown or the pair of node types is wrong
- In the documents: `Error` has `code` and `message` (base: Error schema). Appendix A and the extended specification list each code, and the extended specification gives the status of each code (extended: Errors). The code gives the kind of refusal. Only the message can give the field and the reason. For example, for an `invalid_reference`, only the message can say whether the referenced node does not exist or has the wrong type

## Other gaps

### R33. `/health` answers 200 while a database is stopped

- Status quo: `/health` returns 200 with no content type, so Go sends the response as `text/plain` ([main.go:99-102](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/cmd/server/main.go#L99-L102))
- Observed: with Neo4j stopped, `/health` returned 200 `text/plain` `{"status":"ok"}`
- In the documents: `GetHealth` returns 200 and a JSON `Health` (extended: `GetHealth`). Thus it is a liveness check

### R34. A client that retries `CreateNode` can create a second node

- Status quo: the server makes a new `id` for each `POST .../nodes` ([service.go:59-62](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/internal/graph/service.go#L59-L62)). A client that got no response cannot determine whether the node exists. It can only search for the node, with `ListNodes` or in the changefeed. A retry can create a second node
- Decision: the server makes the `id` of each node, and a client never chooses one
- In the documents: no change. After a lost response, a client can search for the node with `ListNodes` or in the changefeed before it retries `CreateNode`

## Deployment

### R35. If a database starts slowly, the API container exits

- Status quo: Docker Compose starts the Neo4j and PostgreSQL containers, and then starts the API. It does not wait for the databases to accept connections ([docker-compose.yaml:12-16](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/docker-compose.yaml#L12-L16)). The server tries to connect to each database one time ([main.go:41-44](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/cmd/server/main.go#L41-L44), [main.go:53-56](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/cmd/server/main.go#L53-L56)). If an attempt fails, the server exits
- Observed: after `docker compose up`, the API container exited with `connect: connection refused`. After Neo4j logged `Started.`, a second start of the API container succeeded
- In the documents: R35 is about the deployment, and the documents cover the API, so they make no change. A health check on each database service and `condition: service_healthy` on the API service can prevent the failure

## Costs of the changes

- Server: the server code must change as the "In the documents" lines of R1 to R35 describe
- Web client: these parts of `nodoplex-web` must change (links to examples, not to every line):
  - its copy of the OpenAPI document (`src/api/openapi.yaml`), from which `npm run generate` makes the types of every call ([package.json:11](https://github.com/d4nyll/nodoplex-web/blob/d269754ec643ed456f986f61d1bac1167aff89fc/package.json#L11))
  - the create node dialog, which creates the nodes of a sequence first, and then the edges between them ([CreateNodeDialog.tsx:166-196](https://github.com/d4nyll/nodoplex-web/blob/d269754ec643ed456f986f61d1bac1167aff89fc/src/components/NodeForm/CreateNodeDialog.tsx#L166-L196)). It must send `parentIds` with each node, and a parent for the first node unless that node is a Vision
  - the link node dialog ([LinkNodeDialog.tsx:57-66](https://github.com/d4nyll/nodoplex-web/blob/d269754ec643ed456f986f61d1bac1167aff89fc/src/components/NodeForm/LinkNodeDialog.tsx#L57-L66)) and the graph view ([GraphView.tsx:285](https://github.com/d4nyll/nodoplex-web/blob/d269754ec643ed456f986f61d1bac1167aff89fc/src/components/GraphView/GraphView.tsx#L285)), which create edges. They must use the types `hierarchical` and `dependency`, and give the child as the source of a hierarchical edge. The link node dialog reads every status 409 as a dependency cycle, but the documents also give 409 to `invalid_reference`
  - the detail panel, which reads the `parents` and `children` that `GetNode` returns ([DetailPanel.tsx:472-512](https://github.com/d4nyll/nodoplex-web/blob/d269754ec643ed456f986f61d1bac1167aff89fc/src/components/Layout/DetailPanel.tsx#L472-L512))
  - the delete actions of the detail panel, for edges ([DetailPanel.tsx:188-199](https://github.com/d4nyll/nodoplex-web/blob/d269754ec643ed456f986f61d1bac1167aff89fc/src/components/Layout/DetailPanel.tsx#L188-L199)), which must give the edge in the path, and for nodes ([DetailPanel.tsx:201-212](https://github.com/d4nyll/nodoplex-web/blob/d269754ec643ed456f986f61d1bac1167aff89fc/src/components/Layout/DetailPanel.tsx#L201-L212))
  - the unlink buttons of the detail panel, which delete a hierarchical edge and ignore the response ([DetailPanel.tsx:484-494](https://github.com/d4nyll/nodoplex-web/blob/d269754ec643ed456f986f61d1bac1167aff89fc/src/components/Layout/DetailPanel.tsx#L484-L494), [DetailPanel.tsx:521-530](https://github.com/d4nyll/nodoplex-web/blob/d269754ec643ed456f986f61d1bac1167aff89fc/src/components/Layout/DetailPanel.tsx#L521-L530)). With the documents, the delete of the edge to the only parent of a node gets `parent_required`, which the panel must show
  - each `PATCH` request, which must carry the `changeId` that the web client last read ([DetailPanel.tsx:172-175](https://github.com/d4nyll/nodoplex-web/blob/d269754ec643ed456f986f61d1bac1167aff89fc/src/components/Layout/DetailPanel.tsx#L172-L175), [KanbanBoard.tsx:151-154](https://github.com/d4nyll/nodoplex-web/blob/d269754ec643ed456f986f61d1bac1167aff89fc/src/components/KanbanView/KanbanBoard.tsx#L151-L154))
  - the graph view and the detail panel, which read `GET .../subtree` and `GET .../lineage` ([GraphView.tsx:101](https://github.com/d4nyll/nodoplex-web/blob/d269754ec643ed456f986f61d1bac1167aff89fc/src/components/GraphView/GraphView.tsx#L101), [GraphView.tsx:115-116](https://github.com/d4nyll/nodoplex-web/blob/d269754ec643ed456f986f61d1bac1167aff89fc/src/components/GraphView/GraphView.tsx#L115-L116), [DetailPanel.tsx:118](https://github.com/d4nyll/nodoplex-web/blob/d269754ec643ed456f986f61d1bac1167aff89fc/src/components/Layout/DetailPanel.tsx#L118)). They must read `GET .../descendants` and `GET .../ancestors`, which return a `Subgraph`
  - the activity feed, which reads `nextSince` ([ActivityFeed.tsx:56](https://github.com/d4nyll/nodoplex-web/blob/d269754ec643ed456f986f61d1bac1167aff89fc/src/components/ActivityFeed/ActivityFeed.tsx#L56)), and the `nodeId` and the `nodeType` of each change entry ([ActivityFeed.tsx:203-207](https://github.com/d4nyll/nodoplex-web/blob/d269754ec643ed456f986f61d1bac1167aff89fc/src/components/ActivityFeed/ActivityFeed.tsx#L203-L207)). A change entry of an edge has neither of these fields
- Write rate: S1 requires the server to run the writes of one namespace one at a time. The test machine ran 3 runs of 20 sequential `POST .../nodes` requests. The median time per request in each run was from 4.7 ms to 5.6 ms. If each write locks the namespace for approximately that long, one namespace can accept ~180 to ~210 writes each second. Writes to different namespaces can run at the same time

## Where the server keeps the nodes, the edges, and the change entries

- Status quo: at `72c93ef`, Neo4j holds the nodes and their edges. PostgreSQL holds the change entries, which are the records of the changefeed, one for each change to a node. The server writes a node change to Neo4j, and then its change entry to PostgreSQL. The two databases share no transaction. If a failure occurs between the two writes, Neo4j keeps the node change, and PostgreSQL has no change entry for it (R24)
- Why it matters: rule S2 of the base specification requires the changes of an operation and their change entries to be atomic
- Decision: keep both databases. Neo4j keeps the graph, and PostgreSQL keeps the flat list of change entries. Neo4j must not keep every change entry
- In the documents: the server writes each change entry to Neo4j in the transaction of its change, and then copies it to PostgreSQL (extended: B1 to B7). The `changeId` of a node is in Neo4j, so Neo4j must also give each change its change ID. This needs no rewrite of the Cypher queries in `internal/graph/repository.go`, or of the SQL queries in `internal/changelog`
- Cost: the server needs a process that copies the change entries from Neo4j to PostgreSQL. `ListChanges` and `Subscribe` wait for that copy, so they take longer when the copy is behind
- Change data capture (CDC) in Neo4j: Neo4j has a CDC feature, which records the changes to a database. These sources show that CDC is not in the free tiers of Neo4j:
  - The CDC documentation labels CDC for "AuraDB Business Critical", "AuraDB Virtual Dedicated Cloud", and "Enterprise Edition" ([Change Data Capture](https://neo4j.com/docs/cdc/current/))
  - The documentation of the Neo4j Connector for Kafka says: "CDC is not available in Community Edition." ([Quickstart: Source with Change Data Capture](https://neo4j.com/docs/kafka/current/source/quickstart-cdc/))
  - About AuraDB Free, a Neo4j blog post of 2024-08-30 says: "(remember that CDC is not included)" ([Change data capture ... go GA](https://neo4j.com/blog/developer/change-data-capture-cdc-ga/))
  - A Neo4j blog post of 2026-11-02 says: "Change Data Capture (CDC) is available in Aura Virtual Dedicated Cloud, Business Critical, and Neo4j Enterprise." ([Neo4j Connector for Apache Kafka and Confluent v6](https://neo4j.com/blog/developer/neo4j-connector-for-apache-kafka-and-confluent-v6/))
  - The Neo4j pricing page gives AuraDB Business Critical a price "Starting at" "$0.20/GB/hour", and the action "Contact Sales" for self-managed Enterprise Edition ([pricing](https://neo4j.com/pricing/))
- The stack at `72c93ef` runs Neo4j Community Edition (the image `neo4j:2026-community`, [docker-compose.yaml:19](https://github.com/d4nyll/nodoplex/blob/72c93ef3fe2e271585fdbf3858b81feb184ae329/docker-compose.yaml#L19)). Thus the documents keep their own changefeed, and do not use CDC to copy the change entries
