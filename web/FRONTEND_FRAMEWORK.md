# Praetor frontend framework

Praetor's frontend framework is the shared contract for building product surfaces. It is intentionally small: React, Tailwind, the token vocabulary in `tailwind.config.js`, and the components in `components/ui`. It is not a second application framework and does not hide product behavior.

## Rules

1. `index.css` and `tailwind.config.js` are the canonical visual tokens. Pages must use semantic tokens such as `bg-panel`, `text-ink`, `text-mut`, `border-line`, and `text-acc` instead of inventing nearby colors.
2. Every authenticated route starts with `Page` and normally includes one `PageHeader`. Use the `workspace` layout for full-height tables, logs, builders, and split panes.
3. Resource lists put search/filter controls in `PageToolbar` and render loading, empty, and error feedback with the shared state components.
4. Use the UI barrel (`components/ui`) for shared controls. A page-local control is appropriate only when its interaction is unique to that feature.
5. Status must include readable text; color alone is never the status contract.
6. New shared components require interaction and accessibility tests before adoption.

## Page anatomy

```tsx
import { Button, EmptyState, Page, PageHeader, PageToolbar } from '../components/ui';

export function ResourcePage() {
  return (
    <Page>
      <PageHeader
        title="Resources"
        description="Resources available in this organization."
        actions={<Button>New resource</Button>}
      />
      <PageToolbar summary="0 results">
        <input aria-label="Search resources" />
      </PageToolbar>
      <EmptyState title="No resources yet" description="Create the first resource to get started." />
    </Page>
  );
}
```

## Migration sequence

1. Framework foundation: page anatomy, state surfaces, exports, and component tests.
2. Data framework: `DataTable` column definitions, controlled sorting, keyboard row activation, status/data/timestamp values, responsive overflow, and skeleton rows. Pagination and filtering remain page-owned so API behavior stays explicit.
3. Form framework: `FormSection`, focusable validation summaries, duplicate-submit-safe action footers, browser-exit and cancel dirty-state protection, plus write-only `SecretField` controls that cannot accept stored values.
4. Navigation framework: typed route metadata drives breadcrumbs, document titles, search keywords, and command-palette entries. Authorization and capability checks remain separate and server-authoritative.
5. Migrate one representative page and visually validate desktop, narrow desktop, and mobile widths.
6. Migrate remaining pages by resource family; remove superseded page-local patterns as each family lands.

## Definition of done for a migrated page

- Uses the shared page anatomy and semantic tokens.
- Covers loading, empty, error, success, and permission-denied states.
- Keyboard focus order and accessible names are tested.
- No raw API error, secret, query string, or sensitive identifier is rendered.
- `npm test` and `npm run build` pass locally.
- Authenticated route pages remain lazy imports. Run `npm run build:check` to enforce the 250 KiB uncompressed initial-entry budget.

Migrated resource families should include a lightweight architecture test that prevents their superseded page-local loading, header, table, or form structure from being reintroduced during later feature work.
