# Development

Memos is built with a curated tech stack. It is optimized for developer experience and is very easy to start working on the code:

1. It has no external dependency.
2. It requires zero config.
3. 1 command to start backend and 1 command to start frontend, both with live reload support.

## Prerequisites

- [Go](https://golang.org/doc/install)
- [Air](https://github.com/cosmtrek/air#installation) for backend live reload
- [Node.js](https://nodejs.org/)
- [pnpm](https://pnpm.io/installation)
- [buf](https://github.com/bufbuild)

## Steps

1. pull source code

   ```bash
   git clone https://github.com/usememos/memos
   ```

2. start backend using air(with live reload)

   ```bash
   air -c scripts/.air.toml
   ```

3. start frontend dev server

   ```bash
   cd web && pnpm i && pnpm dev
   ```

Memos should now be running at [http://localhost:3001](http://localhost:3001) and change either frontend or backend code would trigger live reload.

### V2 API changes

V2 APIs are using `protobuf`, `buf` is a CLI tool with config files defined.

When changes are made under `proto/api/`:

```bash
cd proto && buf generate
```

Codes will be generated under `proto/gen/`, don't forget to stage and commit these generated codes.
