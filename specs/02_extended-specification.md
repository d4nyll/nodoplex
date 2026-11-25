# Extended Specification: HTTP, JSON, Neo4j, and PostgreSQL

This document extends the [base specification](01_base-specification.md) for this technology stack:

- HTTP/1.1, with JSON bodies, and server-sent events for `Subscribe`
- Neo4j Community Edition, which keeps the Nodes and the Edges
- PostgreSQL 18, which keeps the Change Entries

Each rule of the base specification applies. This document gives each decision that the base specification leaves to an extended specification (see [Decisions left to the extended specification](01_base-specification.md#decisions-left-to-the-extended-specification)). This document defines no Field and no read operation in addition to those of the base specification.

A rule ID that starts with `H`, `I`, `J`, `O`, `X`, `F`, `L`, `B`, or `D` (e.g. H1) names a rule of this document. Each other rule ID (e.g. P1) names a rule of the base specification.

## HTTP

H1. The server must serve HTTP/1.1 (RFC 9112), and must follow the HTTP semantics of RFC 9110.

H2. The server must answer a `HEAD` request as it answers a `GET` request with the same path, query, and headers, but must send no body. The response to a `HEAD` request must end after its headers. For a `HEAD` request to `Subscribe`, the server must run steps 1 to 4 of `Subscribe`, and must then send the status and the headers that F1 gives. The server must not open a subscription for a `HEAD` request.

H3. The server must compare the path of a request with the path of each operation one segment at a time. The server must compare each segment as it receives the segment, before it percent-decodes the segment.

H4. A path parameter (e.g. `{id}`) matches any one segment, empty or not.

H5. To **percent-decode** a text, the server takes the bytes of the text, replaces each `%` and the 2 hexadecimal digits after it with the byte that those digits give, and reads the result as UTF-8. A text is **valid percent-encoding** if each `%` in it has 2 hexadecimal digits after it, and the result of percent-decoding is valid UTF-8.

H6. The server must read the query of a request in these steps:

   1. Split the query at each `&` into parameters. Ignore each empty parameter
   2. Split each parameter at its first `=` into a name and a value. If the parameter has no `=`, its value is empty
   3. In each name and each value, replace each `+` with a space, and then percent-decode the result

H7. Every response must carry these headers:

   - `Access-Control-Allow-Origin: *`
   - `Access-Control-Expose-Headers: Nodoplex-Error-Code`

H8. The response to an `OPTIONS` request must carry these headers:

   - `Access-Control-Allow-Methods: GET, HEAD, PUT, POST, PATCH, DELETE`
   - `Access-Control-Allow-Headers: Content-Type, Last-Event-ID, Nodoplex-User-Id`

H7 and H8 let a browser client on another origin (e.g. Nodoplex Web) call each operation through Cross-Origin Resource Sharing (CORS), as the WHATWG Fetch Standard defines it. The client needs no proxy.

## JSON

J1. Each response body, other than the event stream of `Subscribe`, must be a JSON text (RFC 8259) in UTF-8. Each response with such a body must carry the header `Content-Type: application/json`.

J2. The server must read the body of a `PUT`, `POST`, or `PATCH` request as a JSON text in UTF-8, whatever the content type of the request. The server must read an empty body as `{}`.

J3. The server must ignore the body of a `GET`, `HEAD`, `DELETE`, or `OPTIONS` request.

J4. The server must write each value of a JSON output, and read each value of a JSON input, in the form that the table below gives for its schema type. Each JSON string, including each member name, must hold only Unicode scalar values. Thus a JSON input must not hold a `\u` escape of a code point from U+D800 to U+DFFF that isn't half of a surrogate pair (e.g. `"\ud800"`).

| Schema type                                                                                    | JSON form                                                                                                                                                                                                                                       |
| ---------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| string, and each named schema type that is a string (e.g. `NodeType`, `Status`, `UserId`, `Slug`) | a JSON string                                                                                                                                                                                                                                |
| integer                                                                                        | a JSON number with no fraction part and no exponent part                                                                                                                                                                                        |
| uuid                                                                                           | a JSON string that holds the text form of a UUID (RFC 9562), which is 32 hexadecimal digits in groups of 8, 4, 4, 4, and 12, with a `-` between each group and the next. The server must write each digit in lowercase. An input can write each digit in uppercase or lowercase |
| timestamp                                                                                      | a JSON string that holds an RFC 3339 `date-time` in UTC, with exactly 6 digits after the decimal point of the seconds, and the offset `Z` (e.g. `"2026-11-23T08:15:30.123456Z"`)                                                              |
| boolean                                                                                        | `true` or `false`                                                                                                                                                                                                                               |
| object                                                                                         | a JSON object                                                                                                                                                                                                                                   |
| X[]                                                                                            | a JSON array of values of schema type X                                                                                                                                                                                                         |
| X, nullable                                                                                    | the JSON form of X, or `null` for no value                                                                                                                                                                                                      |
| `Fields`                                                                                       | a JSON object with one member for each Field that the value names. The name of the member is the name of the Field. The value of the member is the JSON form of the value of the Field, or `null` for no value                               |
| a schema (e.g. `Node`, `FilterGroup`)                                                          | a JSON object with one member for each member of the schema that the value gives, with the same name                                                                                                                                           |

J5. In a JSON input, `null` gives no value (e.g. `"description": null` in the `fields` of `UpdateNode` removes the value of `description`). In a JSON output, the server must write `null` only in a value of schema type object.

J6. The JSON form of a timestamp has 6 digits after the decimal point. So when the server takes the time of its clock for C7, the server must truncate that time to whole microseconds.

J7. The **JSON text** of a Change Entry is the JSON form of the Change Entry, with no whitespace outside strings.

Two values of schema type object are the same if they are the same JSON value. Two JSON values are the same if they have the same JSON type, and:

- for two strings: they have the same sequence of Unicode code points
- for two numbers: they are equal as exact decimal numbers (e.g. `1`, `1.0`, and `10e-1` are the same)
- for two booleans: they have the same value
- for two nulls: they are always the same
- for two arrays: they have the same length, and the same values in the same order
- for two objects: they have the same member names, and the same value for each name, in any order of members

Thus, to meet N6, the server must not round a number in a value of schema type object (e.g. by storing the number as a 64-bit floating-point number).

## Operations

The server must serve each operation at the method and the path that the table below gives. A request names its operation by its method and its path. If a request succeeds, the server must send the response that the table gives for the operation.

| Operation           | Method and path                                  | Response to a success                                   |
| ------------------- | ------------------------------------------------ | ------------------------------------------------------- |
| `CreateProduct`     | `PUT /products/{slug}`                           | status 201 and the `Node`                               |
| `ListProducts`      | `GET /products`                                  | status 200 and a `ProductList`                          |
| `CreateNode`        | `POST /products/{slug}/nodes`                    | status 201 and the `Node`                               |
| `GetNode`           | `GET /products/{slug}/nodes/{id}`                | status 200 and the `Node`                               |
| `ListNodes`         | `GET /products/{slug}/nodes`                     | status 200 and a `NodePage`                             |
| `GetNextWork`       | `GET /products/{slug}/next-work`                 | status 200 and a `NextWork`                             |
| `UpdateNode`        | `PATCH /products/{slug}/nodes/{id}`              | status 200 and the `Node`                               |
| `ArchiveNode`       | `POST /products/{slug}/nodes/{id}/archive`       | status 200 and the `Node`                               |
| `UnarchiveNode`     | `POST /products/{slug}/nodes/{id}/unarchive`     | status 200 and the `Node`                               |
| `DeleteNode`        | `DELETE /products/{slug}/nodes/{id}`             | status 204 and no body                                  |
| `WatchNode`         | `POST /products/{slug}/nodes/{id}/watch`         | status 200 and the `Node`                               |
| `UnwatchNode`       | `POST /products/{slug}/nodes/{id}/unwatch`       | status 200 and the `Node`                               |
| `CreateEdge`        | `POST /products/{slug}/edges`                    | status 201 and the `Edge`                               |
| `GetEdge`           | `GET /products/{slug}/edges/{id}`                | status 200 and the `Edge`                               |
| `GetEdgesFrom`      | `GET /products/{slug}/nodes/{id}/edges-from`     | status 200 and an `EdgeList`                            |
| `GetEdgesTo`        | `GET /products/{slug}/nodes/{id}/edges-to`       | status 200 and an `EdgeList`                            |
| `GetHeadNode`       | `GET /products/{slug}/edges/{id}/head`           | status 200 and the `Node`                               |
| `GetTailNode`       | `GET /products/{slug}/edges/{id}/tail`           | status 200 and the `Node`                               |
| `UpdateEdge`        | `PATCH /products/{slug}/edges/{id}`              | status 200 and the `Edge`                               |
| `DeleteEdge`        | `DELETE /products/{slug}/edges/{id}`             | status 204 and no body                                  |
| `GetLatestChangeId` | `GET /products/{slug}/latest-change-id`          | status 200 and a `LatestChangeId`                       |
| `Poll`              | `GET /products/{slug}/poll`                      | status 200 and a `ChangePage`                           |
| `Subscribe`         | `GET /products/{slug}/subscribe`                 | status 200 and an event stream (see [Event stream](#event-stream)) |

O1. To check the `search` filter of `ListNodes`, the server must map each character of the value, of the `title`, and of the `description` to lowercase with the simple case mappings of the Unicode Character Database, and then compare the results.

### Where a request gives its input

I1. The server must read the slug from the path parameter `{slug}`, and the `id` of the input from the path parameter `{id}`. The server must percent-decode each path parameter (H5) before it reads the value.

I2. A string is a valid slug (P1) if it meets each of these constraints, and no other string is a valid slug:

   - It has at most 253 characters
   - It is a sequence of 3 or more labels, with `.` between each label and the next
   - Each label has from 1 to 63 characters, both inclusive
   - Each character of a label is a lowercase ASCII letter (`a` to `z`), an ASCII digit, or `-`
   - No label starts or ends with `-`

   Thus a slug starts with a reverse domain name (e.g. `com.example`) and ends with a product name (e.g. `com.example.language-app`), and needs no percent-encoding in a URL path. The server must not check that the domain exists.

I3. If the method of the operation is `GET` or `DELETE`, the server must read each other member of the input from the query parameter that has the name of the member.

I4. If the method of the operation is `PUT`, `POST`, or `PATCH`, the server must read each other member of the input from the member of the body that has the same name. These members are the **body members** of the operation.

I5. The server must read the user ID from the header `Nodoplex-User-Id`. The server must percent-decode the value of the header (H5), and must use the result as the user ID. For `Subscribe`, if the request doesn't carry the header `Nodoplex-User-Id`, the server must read the user ID from the query parameter `userId`.

   Thus the header can carry each valid user ID, including a user ID that starts with a space.

I6. If a `Subscribe` request carries the header `Last-Event-ID`, the server must read `startChangeId` from that header, and not from the query.

   Thus a browser `EventSource`, which can't set the headers of a request, can give each input of `Subscribe` in the query. When an `EventSource` connects again, it sends the `id` of the last message that it received (F2) in `Last-Event-ID`.

I7. In a path parameter, a query parameter, or a header, the server must read each value in the form that the table below gives for its schema type.

| Schema type                                          | Form                                                                                                                       |
| ---------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| string, and each named schema type that is a string  | the text                                                                                                                   |
| integer                                              | one or more ASCII decimal digits, with a `-` before them if the number is negative                                         |
| uuid                                                 | the text form of a UUID, as J4 gives. Each digit can be uppercase or lowercase                                             |
| boolean                                              | `true` or `false`                                                                                                          |
| (`UserId`, nullable)                                 | the text of the `UserId`, or the empty text for no value. A `UserId` has at least 1 character, so the empty text is never a `UserId` |
| X[], other than the schema type of `filterGroups`    | the query parameter one time for each value, in the order of the values                                                    |

I8. In a query, `filterGroups` is one parameter. Its value is a JSON text that holds the JSON form (J4) of the value of `filterGroups` (e.g. `[{"type":["task"]}]` before percent-encoding).

### How the server checks a request

The server must make these checks on each request, in this order, and must stop at the first check that ends the request. Then the server must make the checks that the base specification gives before step 1 of the operation (see [Operations](01_base-specification.md#operations)).

1. If the method of the request is `OPTIONS`, send status 204 and no body
2. If the path matches the path of no operation, refuse with `not_found`
3. If no operation has both the path and the method of the request, refuse with `method_not_allowed`. For this check, `HEAD` matches each operation whose method is `GET`
4. If a path parameter isn't valid percent-encoding, refuse with `invalid_request`
5. If, at step 3 of H6, a name or a value isn't valid percent-encoding, refuse with `invalid_request`
6. If the name of a query parameter is neither the name of a member that I3 reads from the query, nor `userId` in a `Subscribe` request (I5), refuse with `invalid_request`
7. If a parameter occurs more than one time in the query, and I7 doesn't give that parameter one time for each value of an array, refuse with `invalid_request`
8. If the request carries the header `Nodoplex-User-Id`, and its value isn't valid percent-encoding, refuse with `invalid_request`
9. If the body of a `PUT`, `POST`, or `PATCH` request, or the value of the query parameter `filterGroups`, is above a size limit of the server (e.g. a number of bytes, a depth of nested JSON values), refuse with `invalid_request`
10. If the method of the request is `PUT`, `POST`, or `PATCH`, and the body isn't a JSON text in UTF-8 whose value is an object, refuse with `invalid_request`. J2 reads an empty body as `{}`
11. If the query has the parameter `filterGroups`, and its value isn't a JSON text, refuse with `invalid_request`
12. If a JSON object in the body, or in the value of `filterGroups`, has the same member name more than one time, or a JSON string in them breaks J4, refuse with `invalid_request`
13. If the body has a member that isn't a body member of the operation (I4), refuse with `invalid_request`

## Errors

X1. Each response with a status from 400 to 599, both inclusive, must carry the header `Nodoplex-Error-Code`, whose value is the `code` of the refusal. Unless the method of the request is `HEAD`, the response must also carry an `Error` with that `code` as its body.

X2. A refusal must carry the status that the table below gives for its `code`.

X3. If the server can't parse the request line or the headers of a request, the response may break H7, J1, X1, and X2.

| `code`                   | Status | Condition                                                                                             |
| ------------------------ | ------ | ----------------------------------------------------------------------------------------------------- |
| `invalid_request`        | 400    | as Appendix A of the base specification gives, or a check of this document that names this code       |
| `not_found`              | 404    | as Appendix A gives, or the path matches the path of no operation                                     |
| `method_not_allowed`     | 405    | the path matches the path of an operation, but no operation has both the path and the method of the request |
| `slug_taken`             | 409    | as Appendix A gives                                                                                   |
| `invalid_reference`      | 409    | as Appendix A gives                                                                                   |
| `already_connected`      | 409    | as Appendix A gives                                                                                   |
| `cycle`                  | 409    | as Appendix A gives                                                                                   |
| `parent_required`        | 409    | as Appendix A gives                                                                                   |
| `has_incoming_edges`     | 409    | as Appendix A gives                                                                                   |
| `version_conflict`       | 409    | as Appendix A gives                                                                                   |
| `status_is_derived`      | 409    | as Appendix A gives                                                                                   |
| `done_leaf_blocked`      | 409    | as Appendix A gives                                                                                   |
| `priority_out_of_range`  | 409    | as Appendix A gives                                                                                   |
| `changefeed_disabled`    | 409    | as Appendix A gives                                                                                   |
| `change_out_of_range`    | 409    | as Appendix A gives                                                                                   |
| `change_entries_deleted` | 409    | as Appendix A gives                                                                                   |
| `unavailable`            | 503    | a health check fails (L2, L3)                                                                         |
| `internal_error`         | 500    | as Appendix A gives                                                                                   |

## Event stream

The server must send the output of `Subscribe` as an event stream, in the server-sent events format of the HTML Living Standard. Each line that the server writes to the event stream ends with LF (U+000A).

F1. At step 5 of `Subscribe`, the server must send status 200 and the header `Content-Type: text/event-stream`. The body of the response is the event stream.

F2. For each Change Entry that `Subscribe` pushes, the server must write a message of these lines, in this order:

   1. `id: ` and the value of `changeId` of the Change Entry, in its JSON form (J4)
   2. `data: ` and the JSON text of the Change Entry (J7)
   3. an empty line

   The message has no `event:` line, so its event type is `message`.

F3. To end a subscription with a `code` (e.g. `change_entries_deleted`), the server must write a message of these lines, in this order, and then end the response:

   1. `event: end`
   2. `data: ` and an `Error` with that `code`, in the JSON form that J4 gives, with no whitespace outside strings
   3. an empty line

F4. If the server can't continue a subscription for a reason that has no other `code`, the server must end the subscription with `internal_error` (E1), as F3 gives. If the server can't write to the connection, the server must end the response with no message.

F5. If 30 s pass with no line written to an open event stream, the server must write the line `: keepalive` and an empty line.

F6. To end a subscription, a client closes the connection. When the client closes the connection, the server must end the subscription.

F7. The event stream must hold only the lines that F2, F3, and F5 give.

## Health checks

The server answers 3 requests that aren't operations of the base specification, one for each kind of probe of Kubernetes. Each request takes no input, and needs no header `Nodoplex-User-Id`.

L1. `GET /livez` (liveness probe): the server must send status 200 and no body whenever it can answer an HTTP request.

L2. `GET /startupz` (startup probe): after the server has finished starting (i.e. it has connected to Neo4j and to PostgreSQL, and has created what it needs in them), the server must send status 200 and no body. Before that, the server must refuse with `unavailable`.

L3. `GET /readyz` (readiness probe): for each of these requests, the server must send a new query to Neo4j and a new query to PostgreSQL. If the server has finished starting, and both queries succeed, the server must send status 200 and no body. Otherwise, the server must refuse with `unavailable`. The server may limit how long it waits for each query.

L4. For steps 2 and 3 of [How the server checks a request](#how-the-server-checks-a-request), the path of each of these requests counts as the path of an operation whose method is `GET`. The server must not make steps 4 to 13 on these requests, and must ignore their query.

## Storage

Neo4j keeps the graph, and PostgreSQL keeps the Change Entries. Neo4j Community Edition has no change data capture. So the server writes each Change Entry to Neo4j in the transaction of its write, and then copies the Change Entry to PostgreSQL. B1 to B5 give this transactional outbox.

B1. The server must keep these values in Neo4j:

   - each Node and each Edge
   - for each Changefeed, the latest Change ID (C2), and the `time` of the Change Entry that has that Change ID (C7)
   - the slug of each deleted Product (P3)
   - each Change Entry, from the write that records it until the server deletes it from Neo4j (B5)

B2. A write must write its changes, its Change Entry, the new latest Change ID of the Product, and the `time` of the Change Entry in one Neo4j transaction.

B3. The server must copy each Change Entry from Neo4j to PostgreSQL. In each Changefeed, the server must copy the Change Entries in ascending order of Change ID.

B4. PostgreSQL must hold each Change Entry at most one time.

B5. After the PostgreSQL transaction that holds the copy of a Change Entry commits, the server must delete the Change Entry from Neo4j. This deletion leaves the Change Entry in PostgreSQL, so it isn't a deletion of the Change Entry for C8.

B6. When the server deletes a Change Entry as R2 or P8 gives, the server must delete it from PostgreSQL and from Neo4j. For R2, the server must delete only the Change Entries that it has copied to PostgreSQL.

B7. `Poll`, and each run of step 6 of `Subscribe`, must read the Change Entries from PostgreSQL.

B8. Before a read that B7 gives, the server must read the latest Change ID of the Product from Neo4j. The server must then wait until it has copied to PostgreSQL each Change Entry of the Product whose Change ID is at most that latest Change ID. The read must read only the Change Entries whose Change ID is at most that latest Change ID. `Poll` must return that latest Change ID as `latestChangeId`.

B9. When the server has copied Change Entries of a Product to PostgreSQL, the server must signal each open subscription of that Product. A signal holds no Change Entry. A signal ends the wait of step 7 of `Subscribe`. If the subscription isn't at step 7 when the signal comes, the signal must stay pending, and must end the next wait of step 7 at once.

   Thus the server keeps no buffer of Change Entries for a subscription. One pending signal for each subscription is enough, because each run of step 6 reads from PostgreSQL each Change Entry after the position (B7, B8).

B10. When the server records a Change Entry, the server must make the JSON text of the Change Entry (J7), and must keep that text with the Change Entry in Neo4j and in PostgreSQL. In each output that holds the Change Entry, the server must write that text with no change.

B11. The size of a Change Entry (R1) is the number of bytes of its JSON text.

B12. The server must keep the values that Q5 gives with the Change Entry in Neo4j and in PostgreSQL, outside its JSON text.

## Configuration

D1. When the server starts, the server must read each setting of the table below from its environment variable, as M1 gives. A string is valid if it is in its range. If a setting has no default, and its environment variable isn't set, the server must not start.

| Setting        | Environment variable | Schema type | Default | Comments                                                                                     |
| -------------- | -------------------- | ----------- | ------- | -------------------------------------------------------------------------------------------- |
| Port           | `PORT`               | integer     | `8080`  | from 1 to 65535, both inclusive. The TCP port on which the server accepts HTTP connections   |
| Neo4j URI      | `NEO4J_URI`          | string      | none    | at least 1 character. The URI of the Neo4j server (e.g. `bolt://neo4j:7687`)                 |
| Neo4j user     | `NEO4J_USER`         | string      | none    | at least 1 character. The user name with which the server connects to Neo4j                  |
| Neo4j password | `NEO4J_PASSWORD`     | string      | none    | at least 1 character. The password with which the server connects to Neo4j                   |
| PostgreSQL URL | `POSTGRES_URL`       | string      | none    | at least 1 character. The connection URI of the PostgreSQL database (e.g. `postgres://nodoplex@postgres:5432/nodoplex`) |
