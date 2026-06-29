# Praetor UI Elements Catalog

This document provides a detailed breakdown of the User Interface components and pages within the Praetor platform.

## Overview
The UI is a Single Page Application (SPA) built with React and TypeScript. It uses `react-router-dom` for navigation and standard CSS for styling (bootstrap/custom). State management is primarily handled via React Hooks (`useState`, `useEffect`) within individual pages or lifted to `App.tsx` for global data.

## Pages

### 1. Dashboard ([DashboardPage.tsx](file:///Users/daearol/golang_code/praetor/web/src/pages/DashboardPage.tsx))
**Purpose:** Provides a high-level overview of system status.
-   **Features:**
    -   Displays calculated statistics: Total Jobs, Success Rate, and Failed Jobs.
    -   Lists the 5 most recent job executions with status badges.
-   **Data:** Receives `jobs` array as props.
-   **UI Elements:** Status Cards, Data Table (Recent Activity).

### 2. Jobs ([JobsPage.tsx](file:///Users/daearol/golang_code/praetor/web/src/pages/JobsPage.tsx))
**Purpose:** The central hub for viewing and initiating job executions.
-   **Features:**
    -   **Launch Job:** Dropdown to select a Job Template and a "Launch" button to trigger execution.
    -   **List Jobs:** Tabular view of all previous runs with status and timestamps.
    -   **View Logs:** "Logs" button opens a modal with real-time streaming logs of the selected run.
    -   **Log Viewer:** Modal terminal-style view with ANSI color support for Ansible output.
-   **Data:** `jobs` (list), `templates` (for launch dropdown), `logs` (for modal).
-   **UI Elements:** Select Dropdown, Data Table, Modal (Log Viewer), ANSI-to-HTML converter.

### 3. Templates ([TemplatesPage.tsx](file:///Users/daearol/golang_code/praetor/web/src/pages/TemplatesPage.tsx))
**Purpose:** Manages Job Templates, which define *what* (Playbook) runs *where* (Inventory).
-   **Features:**
    -   **Create/Edit Template:** Form to define Name, Project, Inventory, Playbook file, and Credential.
    -   **Launch Template:** Direct "Launch" button for each template in the list.
-   **Data:** `templates`, `projects`, `inventories`, `credentials`.
-   **UI Elements:** Form (Shared Create/Edit), Data Table, Action Buttons (Edit, Launch).

### 4. Projects ([ProjectsPage.tsx](file:///Users/daearol/golang_code/praetor/web/src/pages/ProjectsPage.tsx))
**Purpose:** Manages Source Control (Git) repositories.
-   **Features:**
    -   **Add Project:** Input fields for Name and Git URL.
    -   **Sync Project:** "Sync" button to trigger a `git pull` on the backend. Shows loading state ("Syncing...") during operation.
-   **Data:** `projects` list. Local state for form inputs.
-   **UI Elements:** Simple Form (Inline), Data Table, Async Action Buttons.

### 5. Inventories ([InventoriesPage.tsx](file:///Users/daearol/golang_code/praetor/web/src/pages/InventoriesPage.tsx))
**Purpose:** Comprehensive management of Hosts and Groups.
-   **Features:**
    -   **Dual-Pane Layout:** Left sidebar for selecting Inventory, right pane for details.
    -   **Host Management:** View, Add, Delete, and Update hosts. Supports defining JSON variables per host.
    -   **Group Management:** Create groups and assign existing hosts to them.
    -   **Control Node Toggle:** Toggle a host as a "Control Node" (Execution Node).
-   **Data:** `inventories`, `hosts`, `groups`, `groupHosts`.
-   **UI Elements:** Split View, Tabs (Hosts/Groups), JSON text input, Interactive List items.

### 6. Credentials ([CredentialsPage.tsx](file:///Users/daearol/golang_code/praetor/web/src/pages/CredentialsPage.tsx))
**Purpose:** Secure storage for authentication secrets.
-   **Features:**
    -   **Dynamic Forms:** Renders input fields based on the selected `CredentialType` schema (e.g., SSH vs AWS).
    -   **Secure Inputs:** Password fields for secrets. Supports "Stored Encrypted" placeholders.
    -   **CRUD:** Create, Edit, Delete credentials.
-   **Data:** `credentials`, `credentialTypes`.
-   **UI Elements:** Dynamic Form Generator, Card Grid View (instead of table).

### 7. Schedules ([SchedulesPage.tsx](file:///Users/daearol/golang_code/praetor/web/src/pages/SchedulesPage.tsx))
**Purpose:** Automates job execution via cron-like schedules.
-   **Features:**
    -   **Create Schedule:** Modal form to select a Template and define frequency (Minute/Hour/Day) or raw RRULE.
    -   **List Schedules:** Shows next run time and status.
    -   **Toggle:** Enable/Disable schedules (UI indicator active, backend logic implied).
-   **Data:** `schedules`, `templates`.
-   **UI Elements:** Modal Form, List View (Styled List Items), Toggle Badges.

### 8. Access Control (RBAC)
Split across three pages:
*   **Users ([UsersPage.tsx](file:///Users/daearol/golang_code/praetor/web/src/pages/UsersPage.tsx)):** Manage system users. Create/Delete users, set Superuser status.
*   **Teams ([TeamsPage.tsx](file:///Users/daearol/golang_code/praetor/web/src/pages/TeamsPage.tsx)):** Group users into Teams. Manage membership via modal.
*   **Roles ([RolesPage.tsx](file:///Users/daearol/golang_code/praetor/web/src/pages/RolesPage.tsx)):** Assign Roles to Users or Teams. "Assign Role" modal to link Subject (User/Team) -> Role.

## Shared Components
*   **[Layout.tsx](file:///Users/daearol/golang_code/praetor/web/src/components/Layout.tsx)**: Wrapper container.
*   **[Sidebar.tsx](file:///Users/daearol/golang_code/praetor/web/src/components/Sidebar.tsx)**: Navigation with `activeTab` highlighting.
*   **[LoginPage.tsx](file:///Users/daearol/golang_code/praetor/web/src/pages/LoginPage.tsx)**: Standalone page for authentication.

## UI Patterns
-   **Data Tables:** Most pages use a standard `<table>` with `<thead>` and `<tbody>`.
-   **Modals:** Custom CSS modals used for Forms (Schedules, Users, Teams) and Log Viewing.
-   **Cards:** Content grouping container (`div.card`).
-   **Badges:** Visual indicators for Status (`successful`, `failed`) and Type (`git`, `Superuser`).
