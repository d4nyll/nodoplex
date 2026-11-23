# Nodoplex architecture

An interactive C4 model of Nodoplex and its web client ([nodoplex-web](https://github.com/d4nyll/nodoplex-web)), written in the [LikeC4](https://likec4.dev) DSL. Each element's description names the source file and functions it stands for.

## View it

From this directory:

```sh
npm ci
npx likec4 start
```

Then open the URL it prints (default http://localhost:5173). Click an element's navigation icon to open the next level:

1. System context (`index`)
2. Containers (`containers`)
3. Components of the API (`api`) and of the web client (`web`)
4. Node labels in the graph database (`graph_schema`)

Flows are under the view list:

- `create_node`: the web client creates a Task, the API appends the change to the changelog, and the activity feed receives it as a server-sent event
- `add_dependency`: the web client adds a `DEPENDS_ON` edge, and the API checks for a dependency cycle under the namespace's lock

Press **Start** in a flow to step through it.

To check the model after an edit, run `npx likec4 validate`. To build a static site, run `npx likec4 build -o dist`.

`likec4` is pinned in `package.json` and needs Node.js 22.22.3 or later.

## Tags

- `#planned`: a proposal plans the element and the code does not have it yet. Planned elements are amber with a dashed border. Today, the only planned element is the Output node, which replaces the GitRef node.
