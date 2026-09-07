# Praetor documentation site

This website is built using [Docusaurus](https://docusaurus.io/), a modern static website generator.

## Installation

```bash
npm ci
```

## Local development

Use Node 22 for the development server. The production build remains validated
on Node 24, but Docusaurus development startup on Node 24 is tracked separately
in [Praetor #372](https://github.com/Niftel/praetor/issues/372).

```bash
npm start
```

This command starts a local development server and opens up a browser window. Most changes are reflected live without having to restart the server.

## Validation

```bash
npm run typecheck
npm run build
```

## Build

```bash
npm run build
```

This command generates static content into the `build` directory and can be served using any static contents hosting service.

## TypeScript 7 compatibility

The documentation site uses TypeScript 7. Docusaurus 3.10.2's published
`@docusaurus/tsconfig` still defines the removed `baseUrl` compiler option, so
the site owns the equivalent compiler configuration in `tsconfig.json`.

The local configuration follows the
[Docusaurus upstream TypeScript 7 change](https://github.com/facebook/docusaurus/commit/54b8b4df9c0d66821a23437600a4858c92ddb4ef):
it maps `@site/*` through `${configDir}` and does not suppress removed compiler
options. The migration risk is configuration drift while Praetor remains on
Docusaurus 3.10.2.

When Docusaurus publishes this configuration, replace the local copy with
`extends: "@docusaurus/tsconfig"` and restore the direct package dependency
after the clean install, typecheck, development startup, and production build
all pass.
