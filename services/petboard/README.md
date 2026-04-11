# petboard

Personal project tracker for attlas. Reachable at
`https://attlas.uk/petboard/` once installed. Managed interactively by
Claude Code via an MCP endpoint served from the same Go binary.

See [PLAN.md](./PLAN.md) for the full design: data model, canvas UX,
auth architecture, install layout, and implementation order.

## Layout

```
services/petboard/
├── server/              # Go backend — REST + SSE + OAuth 2.1 + MCP
│   ├── cmd/petboard/    # main entry point
│   └── db/              # SQLite migrations + bootstrap seed
└── web/                 # React + Vite + Tailwind + react-konva frontend
```

## Install

```
sudo bash services/petboard/install-petboard.sh
```

Registers petboard's public paths in
`/etc/attlas-public-paths.d/petboard.conf`, signals alive-server to
reload (`systemctl kill --signal=SIGHUP alive-server`), and drops a
route snippet in `/etc/caddy/conf.d/petboard.caddy`.
