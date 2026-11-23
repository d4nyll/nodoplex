# Extended Specification: HTTP and JSON

This document extends the [base specification](01_base-specification.md) for HTTP, JSON, server-sent events, and storage. Every requirement of the base specification applies, except that X3 and X4 allow the server to break E1. This document adds the requirements that the base specification leaves to an extended specification, under [Decisions left to the extended specification](01_base-specification.md#decisions-left-to-the-extended-specification).

## HTTP

H1. The server must follow the HTTP semantics of RFC 9110.

H2. The server must answer a `HEAD` request as it answers a `GET` request with the same path, query, and headers, but must omit the body. The response to a `HEAD` request must end after its headers.

H3. The server must match each segment of the path of a request as received, before it percent-decodes any segment.

H4. The server must match a path parameter to any one path segment, empty or not.

H5. The server must percent-decode each path parameter before it checks the value.

H6. The server must decode the query of a request as `application/x-www-form-urlencoded`, as the WHATWG URL Standard defines it.

H7. Every response must carry the header `Access-Control-Allow-Origin: *`.

H8. The response to an `OPTIONS` request must carry the headers `Access-Control-Allow-Methods: GET, HEAD, POST, PATCH, DELETE` and `Access-Control-Allow-Headers: Content-Type, Last-Event-ID`.

## Operations

The server must serve each operation at the method and the path that the table below gives. The response to a request that succeeds must be the response that the table gives.

| Operation         | Method and path                                                      | Response to a success                                                         |
| ----------------- | -------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| `CreateNode`      | `POST /namespaces/{namespace}/nodes`                                 | status 201 and the `Node`                                                     |
| `GetNode`         | `GET /namespaces/{namespace}/nodes/{id}`                             | status 200 and the `Node`                                                     |
| `ListNodes`       | `GET /namespaces/{namespace}/nodes`                                  | status 200 and a `NodeList`                                                   |
| `UpdateNode`      | `PATCH /namespaces/{namespace}/nodes/{id}`                           | status 200 and the `Node`                                                     |
| `DeleteNode`      | `DELETE /namespaces/{namespace}/nodes/{id}`                          | status 204 and no body                                                        |
| `CreateEdge`      | `POST /namespaces/{namespace}/edges`                                 | status 201 and the `Edge` after step 7, or status 200 and the `Edge` from step 5 |
| `DeleteEdge`      | `DELETE /namespaces/{namespace}/edges/{type}/{sourceId}/{targetId}`  | status 204 and no body                                                        |
| `GetParents`      | `GET /namespaces/{namespace}/nodes/{id}/parents`                     | status 200 and a `NodeArray`                                                  |
| `GetChildren`     | `GET /namespaces/{namespace}/nodes/{id}/children`                    | status 200 and a `NodeArray`                                                  |
| `GetAncestors`    | `GET /namespaces/{namespace}/nodes/{id}/ancestors`                   | status 200 and a `Subgraph`                                                   |
| `GetDescendants`  | `GET /namespaces/{namespace}/nodes/{id}/descendants`                 | status 200 and a `Subgraph`                                                   |
| `GetDependencies` | `GET /namespaces/{namespace}/nodes/{id}/dependencies`                | status 200 and a `NodeArray`                                                  |
| `GetDependents`   | `GET /namespaces/{namespace}/nodes/{id}/dependents`                  | status 200 and a `NodeArray`                                                  |
| `ListChanges`     | `GET /namespaces/{namespace}/changes`                                | status 200 and a `ChangeList`                                                 |
| `Subscribe`       | `GET /namespaces/{namespace}/subscribe`                              | status 200 and an event stream (see [Event stream](#event-stream))            |
| `GetHealth`       | `GET /health`                                                        | status 200 and a `Health`                                                     |

I1. The server must read the namespace of a request from the path segment after `/namespaces/`.

I2. The server must read each input field that the path names from the path.

I3. The server must read each other input field of a `GET` operation from the query.

I4. The server must read each other input field of a `POST` or `PATCH` operation from the body. These fields are the **body fields** of the operation.

I5. If a `Subscribe` request carries the header `Last-Event-ID`, the server must read `since` from that header, and not from the query.

### How the server checks a request

The server must make these checks in this order, and must stop at the first check that ends the request:

1. If the method of the request is `OPTIONS`, return status 204 and no body
2. If the path matches no operation, refuse with `not_found`
3. If no operation matches both the path and the method of the request, refuse with `method_not_allowed`. For this check, `HEAD` matches each operation whose method is `GET`
4. If a path parameter is not valid percent-encoding, refuse with `invalid_request`
5. If a query parameter is not an input field that I3 reads from the query, refuse with `invalid_request`
6. If a query parameter occurs more than one time, and its schema type is not X[], refuse with `invalid_request`
7. If the operation is a `POST` or `PATCH` operation:
   1. If the body or a value in it breaks a size or depth limit of the implementation, refuse with `invalid_request`
   2. If the body is not a JSON object, refuse with `invalid_request`
   3. If an object in the body has the same member name more than one time, refuse with `invalid_request`
   4. If a member of the body is not a body field of the operation, refuse with `invalid_request`. For this check, the body fields of `CreateNode` and `UpdateNode` are those of every node type

After these checks, for an operation of the base specification, the server must make the checks of the base specification before step 1.

### `GetHealth`

Output: a `Health`.

| Field    | Schema type | Comments    |
| -------- | ----------- | ----------- |
| `status` | string      | always `ok` |

Steps:

1. Return status 200 and a `Health`

## JSON

J1. Every response body other than the event stream of `Subscribe` must be JSON (RFC 8259) in UTF-8.

J2. Every response with a JSON body must carry the content type `application/json`.

J3. The server must read the body of a `POST` or `PATCH` request as JSON in UTF-8, whatever the content type.

J4. The server must ignore the body of a request to an operation that is not a `POST` or `PATCH` operation.

J5. In a response, a field of a node, an edge, or a change entry must not have the value `null`.

J6. The server must write each schema as a JSON object whose members are the fields of the schema, with the same names.

The schema types of the base specification have these forms:

| Schema type | Form                                                                                                                       |
| ----------- | -------------------------------------------------------------------------------------------------------------------------- |
| string      | in JSON, a JSON string. In a path or a query parameter, the decoded text                                                  |
| integer     | in JSON, a number with no fraction part and no exponent. In a path, a query parameter, or a header, one or more decimal ASCII digits, with a `-` before them if the number is negative |
| uuid        | the text form of a UUID in RFC 9562, for any 128-bit value. The text form has 32 hexadecimal digits in groups of 8, 4, 4, 4, and 12, with a `-` between groups. The server must write the digits in lower case. A request is allowed to use any mix of upper and lower case |
| date        | a string in the form `YYYY-MM-DD` (the `full-date` of RFC 3339)                                                           |
| timestamp   | a string that holds an RFC 3339 `date-time` in UTC, with the offset written as `Z`                                        |
| object      | a JSON object                                                                                                              |
| X[]         | in JSON, an array of values of schema type X. In a query, a query parameter that occurs one time for each value           |

In a body, a field with the value `null` is a field with no value.

Two values of schema type object are the same if they are the same JSON value. Two JSON values are the same if they have the same JSON type, and:

- for two strings: they have the same sequence of Unicode code points
- for two numbers: they are equal as exact decimal numbers
- for two booleans: they have the same value
- for two nulls: they are always the same
- for two arrays: they have the same length, and the same values in the same order
- for two objects: they have the same member names, and the same value for each name

## Errors

X1. A response with a status from 400 to 599, both inclusive, must carry an `Error` body, except a response to `HEAD`.

X2. A refusal must carry the status that the table below gives for its `code`.

X3. After the server sends the status of a response, the server is allowed to break E1 of the base specification.

X4. If the server refuses a request before it parses the request line and the headers, the response is allowed to break these rules:

   - J1
   - J2
   - H7
   - X1
   - X2
   - E1 of the base specification

| `code`                | Status | Condition                                                                                            |
| --------------------- | ------ | ---------------------------------------------------------------------------------------------------- |
| `invalid_request`     | 400    | as in the base specification, or a check of this document that names this code                      |
| `not_found`           | 404    | as in the base specification, or the path matches no operation                                       |
| `method_not_allowed`  | 405    | the path matches an operation, but no operation matches both the path and the method of the request |
| `invalid_reference`   | 409    | as in the base specification                                                                         |
| `dependency_cycle`    | 409    | as in the base specification                                                                         |
| `parent_required`     | 409    | as in the base specification                                                                         |
| `change_id_mismatch`  | 409    | as in the base specification                                                                         |
| `change_out_of_range` | 409    | as in the base specification                                                                         |
| `internal_error`      | 500    | as in the base specification                                                                         |

## Event stream

The server must send the output of `Subscribe` as an event stream in the server-sent events format of the HTML Living Standard.

F1. At step 3 of `Subscribe`, the server must send status 200 with the content type `text/event-stream`.

F2. The server must write the start message as the line `id: ` with the start change ID, and then an empty line.

F3. The server must write each message with a change entry as these lines, in this order:

   1. `id: ` and the value of `changeId`
   2. `event: `, the value of `entity`, `.`, and the value of `action` (e.g. `event: node.created`)
   3. `data: ` and the change entry as JSON on one line
   4. an empty line

F4. If 30 s pass with no line written to an open event stream, the server must write a `: keepalive` line and an empty line.

F5. The stream must hold only the lines that F2, F3, and F4 give.

F6. If the server cannot continue an event stream, the server must end the response that carries the event stream.

F7. If the client closes the connection of a `Subscribe` request, the server must end the event stream.

## Storage

B1. The server must keep the nodes, the edges, and the change entries in one database.

B2. The server must write the changes of an operation and its change entries in one transaction of that database.
