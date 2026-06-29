# AWX Organizations, Users, and Teams - Complete Reference

This document provides comprehensive information about how Organizations, Users, and Teams function in AWX, including their models, relationships, permissions, API endpoints, and RBAC system.

---

## Table of Contents

1. [Organizations](#organizations)
2. [Users](#users)
3. [Teams](#teams)
4. [Role-Based Access Control (RBAC)](#role-based-access-control-rbac)
5. [Relationships and Membership](#relationships-and-membership)
6. [API Endpoints](#api-endpoints)
7. [Access Control and Permissions](#access-control-and-permissions)

---

## Organizations

### Overview

**Organizations** are the basic unit of multi-tenancy divisions in AWX. They provide logical separation of resources, users, and teams within the system.

### Model Definition

**Location**: `awx/main/models/organization.py`

```python
class Organization(CommonModel, NotificationFieldsModel, ResourceMixin, 
                  CustomVirtualEnvMixin, RelatedJobsMixin, OpaQueryPathMixin):
    """
    An organization is the basic unit of multi-tenancy divisions
    """
```

### Fields

#### Core Fields (from CommonModel)
- `id` - Primary key
- `name` - Organization name (required, unique)
- `description` - Optional description
- `created` - Creation timestamp
- `modified` - Last modification timestamp
- `created_by` - User who created the organization
- `modified_by` - User who last modified the organization

#### Organization-Specific Fields
- `max_hosts` - Maximum number of hosts allowed to be managed by this organization (PositiveIntegerField, default=0)
- `default_environment` - Default execution environment for jobs run by this organization (ForeignKey to ExecutionEnvironment, nullable)
- `custom_virtualenv` - Custom Python virtual environment path (read-only, from CustomVirtualEnvMixin)
- `opa_query_path` - OPA query path for policy evaluation (from OpaQueryPathMixin)

#### Relationships
- `instance_groups` - Many-to-many relationship with InstanceGroup (through OrganizationInstanceGroupMembership)
- `galaxy_credentials` - Many-to-many relationship with Credential for Galaxy API tokens (through OrganizationGalaxyCredentialMembership)
- `notification_templates_approvals` - Many-to-many relationship with NotificationTemplate for workflow approvals
- `teams` - One-to-many relationship (Team.organization)
- `inventories` - One-to-many relationship (Inventory.organization)
- `projects` - One-to-many relationship (Project.organization)
- `job_templates` - One-to-many relationship (JobTemplate.organization)
- `workflow_job_templates` - One-to-many relationship (WorkflowJobTemplate.organization)
- `credentials` - One-to-many relationship (Credential.organization)
- `execution_environments` - One-to-many relationship (ExecutionEnvironment.organization)
- `notification_templates` - One-to-many relationship (NotificationTemplate.organization)
- `labels` - One-to-many relationship (Label.organization)

### Roles

Organizations have a comprehensive role hierarchy:

1. **admin_role** - Full administrative control
   - Parent: System Administrator singleton role
   - Can manage all aspects of the organization

2. **execute_role** - Can execute jobs
   - Parent: admin_role
   - May run any executable resources in the organization

3. **project_admin_role** - Project administration
   - Parent: admin_role
   - Can manage all projects of the organization

4. **inventory_admin_role** - Inventory administration
   - Parent: admin_role
   - Can manage all inventories of the organization

5. **credential_admin_role** - Credential administration
   - Parent: admin_role
   - Can manage all credentials of the organization

6. **workflow_admin_role** - Workflow administration
   - Parent: admin_role
   - Can manage all workflows of the organization

7. **notification_admin_role** - Notification administration
   - Parent: admin_role
   - Can manage all notifications of the organization

8. **job_template_admin_role** - Job template administration
   - Parent: admin_role
   - Can manage all job templates of the organization

9. **execution_environment_admin_role** - Execution environment administration
   - Parent: admin_role
   - Can manage all execution environments of the organization

10. **auditor_role** - Read-only audit access
    - Parent: System Auditor singleton role
    - Can view all aspects of the organization

11. **member_role** - Basic membership
    - Parent: admin_role
    - User is a member of the organization

12. **read_role** - Read access
    - Parent: member_role, auditor_role, execute_role, and all admin roles
    - May view settings for the organization

13. **approval_role** - Workflow approval
    - Parent: admin_role
    - Can approve or deny workflow approval nodes

### Permissions

- `member_organization` - Basic participation permissions for organization
- `audit_organization` - Audit everything inside the organization
- Default permissions: `change`, `delete`, `view` (add permission removed - only superuser can add)

### Access Control

**Location**: `awx/main/access.py` - `OrganizationAccess`

#### Visibility Rules
- Superuser: Can see all organizations
- Organization admin: Can see organizations they admin
- Organization member: Can see organizations they are members of

#### Modification Rules
- Superuser: Can change/delete any organization
- Organization admin: Can change/delete organizations they admin

#### Special Rules
- Only superusers can create organizations
- Only superusers can modify `max_hosts` field
- Instance group associations require admin role and use_role on instance group

---

## Users

### Overview

**Users** are Django's built-in User model extended with AWX-specific functionality. Users can be members of organizations and teams, and can have various roles assigned.

### Model Definition

**Base**: Django's `django.contrib.auth.models.User`

**Extensions**: Custom methods added via `User.add_to_class()` in `awx/main/models/__init__.py`

### Fields

#### Standard Django User Fields
- `id` - Primary key
- `username` - Required, 150 characters or fewer, unique
- `password` - Hashed password (write-only in API)
- `email` - Email address
- `first_name` - First name
- `last_name` - Last name
- `is_superuser` - Boolean, designates superuser status
- `is_staff` - Boolean, designates staff status
- `is_active` - Boolean, designates active status
- `date_joined` - Account creation timestamp
- `last_login` - Last login timestamp

#### AWX-Specific Fields
- `is_system_auditor` - Boolean, system-wide read-only access
- `resource` - AnsibleResourceField for resource registry integration

### Extended Methods

Custom methods added to User model:
- `get_queryset()` - Get queryset filtered by user permissions
- `can_access()` - Check if user can access a resource
- `can_access_with_errors()` - Check access with detailed error information
- `summary_fields` - Generate summary fields for API responses

### Relationships

- **Organizations**: Many-to-many through `Organization.member_role.members` and `Organization.admin_role.members`
- **Teams**: Many-to-many through `Team.member_role.members` and `Team.admin_role.members`
- **Roles**: Many-to-many through `Role.members`
- **Projects**: Indirect through organizations and teams
- **Credentials**: Can own credentials directly
- **Activity Stream**: All user actions are logged

### Access Control

**Location**: `awx/main/access.py` - `UserAccess`

#### Visibility Rules
- Superuser: Can see all users
- Organization members: Can see users in same organizations (if `ORG_ADMINS_CAN_SEE_ALL_USERS` setting enabled, org admins can see all users)
- Self: Users can always see themselves
- System users: Superusers are always visible

#### Creation Rules
- Superuser: Can create any user
- Organization admin: Can create users (if `MANAGE_ORGANIZATION_AUTH` setting enabled)
- Cannot create superuser or system auditor unless requester is superuser

#### Modification Rules
- Self: Users can change their own password and some fields
- Superuser: Can change any user
- Organization admin: Can change users in their organizations (if `MANAGE_ORGANIZATION_AUTH` enabled)
- Cannot change superuser status unless requester is superuser
- Cannot change system auditor status unless requester is superuser or the user themselves

#### Deletion Rules
- Superuser: Can delete users (except themselves)
- Organization admin: Can delete users in their organizations
- Users cannot delete themselves

### Password Validation

The UserSerializer enforces password validation:
- Required for new users
- Maximum length validation
- Minimum length (if `LOCAL_PASSWORD_MIN_LENGTH` configured)
- Minimum digits (if `LOCAL_PASSWORD_MIN_DIGITS` configured)
- Minimum uppercase characters (if `LOCAL_PASSWORD_MIN_UPPER` configured)
- Minimum special characters (if `LOCAL_PASSWORD_MIN_SPECIAL` configured)
- Django password validators (if `AUTH_PASSWORD_VALIDATORS` configured)

---

## Teams

### Overview

**Teams** are groups of users within an organization that work on common projects. Teams provide a way to organize users and assign permissions at a more granular level than organizations.

### Model Definition

**Location**: `awx/main/models/organization.py`

```python
class Team(CommonModelNameNotUnique, ResourceMixin):
    """
    A team is a group of users that work on common projects.
    """
```

### Fields

#### Core Fields (from CommonModelNameNotUnique)
- `id` - Primary key
- `name` - Team name (required, unique within organization)
- `description` - Optional description
- `created` - Creation timestamp
- `modified` - Last modification timestamp
- `created_by` - User who created the team
- `modified_by` - User who last modified the team

#### Team-Specific Fields
- `organization` - ForeignKey to Organization (required, cannot be changed after creation)
- `resource` - AnsibleResourceField for resource registry integration

### Constraints

- **Unique Together**: `(organization, name)` - Team names must be unique within an organization
- **Organization Immutability**: Teams cannot be moved to a different organization after creation

### Roles

Teams have a simpler role hierarchy than organizations:

1. **admin_role** - Team administration
   - Parent: `organization.admin_role`
   - Can manage all aspects of the team

2. **member_role** - Team membership
   - Parent: `admin_role`
   - User is a member of the team

3. **read_role** - Read access
   - Parent: `organization.auditor_role`, `member_role`
   - May view settings for the team

### Permissions

- `member_team` - Inherit all roles assigned to this team

### Relationships

- **Organization**: Many-to-one (Team.organization) - Required, cannot be null
- **Users**: Many-to-many through `Team.member_role.members` and `Team.admin_role.members`
- **Projects**: Indirect through roles assigned to teams
- **Credentials**: Teams can own credentials
- **Roles**: Teams can be assigned roles on resources

### Access Control

**Location**: `awx/main/access.py` - `TeamAccess`

#### Visibility Rules
- Superuser: Can see all teams
- Team admin: Can see teams they admin
- Team member: Can see teams they are members of
- Organization member: Can see teams in their organizations (if `ORG_ADMINS_CAN_SEE_ALL_USERS` enabled)

#### Creation Rules
- Superuser: Can create teams
- Organization admin: Can create teams in their organizations (if `MANAGE_ORGANIZATION_AUTH` enabled)
- Must specify a valid organization

#### Modification Rules
- Superuser: Can change any team
- Team admin: Can change teams they admin
- Cannot change organization (teams cannot be moved between organizations)

#### Deletion Rules
- Same as modification rules

#### Membership Management
- Team admins can add/remove members
- Organization admins can manage team membership
- Role assignments to teams are handled through RoleAccess

---

## Role-Based Access Control (RBAC)

### Overview

AWX uses a sophisticated Role-Based Access Control (RBAC) system that provides fine-grained permissions through role hierarchies and implicit role inheritance.

### Role Model

**Location**: `awx/main/models/rbac.py`

```python
class Role(models.Model):
    role_field = models.TextField(null=False)  # e.g., 'admin_role', 'member_role'
    singleton_name = models.TextField(null=True, unique=True)  # For system-wide roles
    parents = models.ManyToManyField('Role', related_name='children')
    implicit_parents = models.TextField(null=False, default='[]')
    ancestors = models.ManyToManyField('Role', through='RoleAncestorEntry')
    members = models.ManyToManyField('auth.User', related_name='roles')
    content_type = models.ForeignKey(ContentType, null=True)
    object_id = models.PositiveIntegerField(null=True)
    content_object = GenericForeignKey('content_type', 'object_id')
```

### Role Types

#### System-Wide Roles (Singletons)
- **system_administrator** - Full system access
- **system_auditor** - Read-only system access

#### Organization Roles
- `admin_role` - Organization administrator
- `member_role` - Organization member
- `auditor_role` - Organization auditor
- `execute_role` - Can execute jobs
- `project_admin_role` - Project administrator
- `inventory_admin_role` - Inventory administrator
- `credential_admin_role` - Credential administrator
- `workflow_admin_role` - Workflow administrator
- `notification_admin_role` - Notification administrator
- `job_template_admin_role` - Job template administrator
- `execution_environment_admin_role` - Execution environment administrator
- `read_role` - Read access
- `approval_role` - Workflow approval

#### Team Roles
- `admin_role` - Team administrator
- `member_role` - Team member
- `read_role` - Read access

#### Resource Roles
- `admin_role` - Full control
- `use_role` - Can use in job templates
- `update_role` - Can update
- `read_role` - Can read
- `execute_role` - Can execute
- `adhoc_role` - Can run ad hoc commands

### Role Hierarchy

Roles form a hierarchical structure where:
- **Parent roles** grant permissions to **child roles**
- **Ancestor roles** are automatically computed and cached
- Users in a role inherit permissions from all ancestor roles

### Role Assignment

#### To Users
- Direct assignment: User added to role's `members` ManyToManyField
- Indirect assignment: User inherits through team membership

#### To Teams
- Teams can be assigned roles on resources
- All team members inherit the team's roles
- Teams cannot be assigned organization participation roles (admin_role, member_role) - these are user-only

### Role Evaluation

The `Role.__contains__()` method determines if a user is in a role:
- Superusers are in all roles
- System auditors are in all read/auditor roles
- Direct membership checked via `ancestors.filter(members=user)`
- Integration with ansible-base RBAC system when enabled

### Implicit Roles

Implicit roles are automatically created based on model relationships:
- Defined via `ImplicitRoleField` on models
- Automatically maintained when relationships change
- Parent roles specified in field definition

---

## Relationships and Membership

### Organization ↔ User

#### User Membership in Organizations
- Users are added to organizations via `Organization.member_role.members.add(user)`
- Users can be organization admins via `Organization.admin_role.members.add(user)`
- A user can be a member of multiple organizations
- Users can be admins of multiple organizations

#### API Endpoints
- `GET /api/v2/organizations/{id}/users/` - List organization members
- `GET /api/v2/organizations/{id}/admins/` - List organization admins
- `POST /api/v2/organizations/{id}/users/` - Add user to organization
- `POST /api/v2/organizations/{id}/admins/` - Add user as admin
- `DELETE /api/v2/organizations/{id}/users/{user_id}/` - Remove user
- `GET /api/v2/users/{id}/organizations/` - List user's organizations
- `GET /api/v2/users/{id}/admin_of_organizations/` - List organizations user admins

### Organization ↔ Team

#### Team Membership in Organizations
- Teams belong to exactly one organization (required ForeignKey)
- Teams cannot be moved between organizations
- Teams inherit permissions from their organization's admin_role

#### API Endpoints
- `GET /api/v2/organizations/{id}/teams/` - List organization teams
- `POST /api/v2/organizations/{id}/teams/` - Create team in organization
- `GET /api/v2/teams/{id}/` - Team detail (includes organization)

### Team ↔ User

#### User Membership in Teams
- Users are added to teams via `Team.member_role.members.add(user)`
- Users can be team admins via `Team.admin_role.members.add(user)`
- A user can be a member of multiple teams
- Users can be admins of multiple teams
- Team membership is scoped to the team's organization

#### API Endpoints
- `GET /api/v2/teams/{id}/users/` - List team members
- `POST /api/v2/teams/{id}/users/` - Add user to team
- `DELETE /api/v2/teams/{id}/users/{user_id}/` - Remove user from team
- `GET /api/v2/users/{id}/teams/` - List user's teams

### Role Assignment Flow

1. **Direct User Assignment**
   - User added directly to resource role
   - User immediately has role permissions

2. **Team Assignment**
   - Team assigned role on resource
   - All team members inherit the role
   - Changes propagate automatically

3. **Organization Inheritance**
   - Organization roles grant permissions to organization resources
   - Organization admins can manage organization resources
   - Organization members have read access to organization resources

---

## API Endpoints

### Organizations

#### Collection Endpoints
- `GET /api/v2/organizations/` - List organizations
- `POST /api/v2/organizations/` - Create organization (superuser only)

#### Detail Endpoints
- `GET /api/v2/organizations/{id}/` - Organization detail
- `PUT /api/v2/organizations/{id}/` - Update organization
- `PATCH /api/v2/organizations/{id}/` - Partial update
- `DELETE /api/v2/organizations/{id}/` - Delete organization

#### Related Resource Endpoints
- `GET /api/v2/organizations/{id}/users/` - Organization users
- `GET /api/v2/organizations/{id}/admins/` - Organization admins
- `GET /api/v2/organizations/{id}/teams/` - Organization teams
- `GET /api/v2/organizations/{id}/inventories/` - Organization inventories
- `GET /api/v2/organizations/{id}/projects/` - Organization projects
- `GET /api/v2/organizations/{id}/job_templates/` - Organization job templates
- `GET /api/v2/organizations/{id}/workflow_job_templates/` - Organization workflows
- `GET /api/v2/organizations/{id}/credentials/` - Organization credentials
- `GET /api/v2/organizations/{id}/execution_environments/` - Execution environments
- `GET /api/v2/organizations/{id}/instance_groups/` - Instance groups
- `GET /api/v2/organizations/{id}/galaxy_credentials/` - Galaxy credentials
- `GET /api/v2/organizations/{id}/notification_templates/` - Notification templates
- `GET /api/v2/organizations/{id}/activity_stream/` - Activity stream
- `GET /api/v2/organizations/{id}/object_roles/` - Organization roles
- `GET /api/v2/organizations/{id}/access_list/` - Access list (deprecated)

### Users

#### Collection Endpoints
- `GET /api/v2/users/` - List users
- `POST /api/v2/users/` - Create user

#### Detail Endpoints
- `GET /api/v2/users/{id}/` - User detail
- `PUT /api/v2/users/{id}/` - Update user
- `PATCH /api/v2/users/{id}/` - Partial update
- `DELETE /api/v2/users/{id}/` - Delete user

#### Current User
- `GET /api/v2/me/` - Current authenticated user info

#### Related Resource Endpoints
- `GET /api/v2/users/{id}/teams/` - User teams
- `GET /api/v2/users/{id}/organizations/` - User organizations
- `GET /api/v2/users/{id}/admin_of_organizations/` - Organizations user admins
- `GET /api/v2/users/{id}/projects/` - User projects
- `GET /api/v2/users/{id}/credentials/` - User credentials
- `GET /api/v2/users/{id}/roles/` - User roles
- `GET /api/v2/users/{id}/activity_stream/` - Activity stream
- `GET /api/v2/users/{id}/access_list/` - Access list (deprecated)

### Teams

#### Collection Endpoints
- `GET /api/v2/teams/` - List teams
- `POST /api/v2/teams/` - Create team

#### Detail Endpoints
- `GET /api/v2/teams/{id}/` - Team detail
- `PUT /api/v2/teams/{id}/` - Update team
- `PATCH /api/v2/teams/{id}/` - Partial update
- `DELETE /api/v2/teams/{id}/` - Delete team

#### Related Resource Endpoints
- `GET /api/v2/teams/{id}/users/` - Team users
- `POST /api/v2/teams/{id}/users/` - Add user to team
- `DELETE /api/v2/teams/{id}/users/{user_id}/` - Remove user from team
- `GET /api/v2/teams/{id}/roles/` - Team roles (deprecated)
- `GET /api/v2/teams/{id}/object_roles/` - Team object roles (deprecated)
- `GET /api/v2/teams/{id}/projects/` - Team projects
- `GET /api/v2/teams/{id}/credentials/` - Team credentials
- `GET /api/v2/teams/{id}/activity_stream/` - Activity stream
- `GET /api/v2/teams/{id}/access_list/` - Access list (deprecated)

### Roles

#### Collection Endpoints
- `GET /api/v2/roles/` - List roles
- `GET /api/v2/roles/{id}/` - Role detail

#### Role Assignment
- `GET /api/v2/roles/{id}/users/` - Role users
- `POST /api/v2/roles/{id}/users/` - Assign users to role
- `GET /api/v2/roles/{id}/teams/` - Role teams
- `POST /api/v2/roles/{id}/teams/` - Assign teams to role

---

## Access Control and Permissions

### Permission System Architecture

AWX uses a multi-layered permission system:

1. **Django Permissions** - Model-level permissions
2. **RBAC Roles** - Object-level role assignments
3. **Access Classes** - Custom permission logic in `awx/main/access.py`

### Access Class Pattern

Each model has a corresponding access class that implements:
- `filtered_queryset()` - Filter queryset by user permissions
- `can_add(data)` - Check if user can create resource
- `can_change(obj, data)` - Check if user can modify resource
- `can_delete(obj)` - Check if user can delete resource
- `can_attach(obj, sub_obj, relationship)` - Check if user can attach related object
- `can_unattach(obj, sub_obj, relationship)` - Check if user can detach related object

### Permission Checking Flow

1. **Request arrives** → Django REST Framework view
2. **Permission class checks** → `ModelAccessPermission.has_permission()`
3. **Access class checks** → `{Model}Access.can_{action}()`
4. **Role evaluation** → `Role.__contains__(user)`
5. **Ancestor traversal** → Check role ancestors for membership
6. **Decision** → Allow or deny with appropriate HTTP status

### Common Permission Patterns

#### Superuser Bypass
- Superusers typically bypass permission checks (unless `always_allow_superuser=False`)
- Decorated with `@check_superuser` in access classes

#### Organization Scoping
- Most resources are scoped to organizations
- Users can only access resources in their organizations
- Organization admins have elevated permissions

#### Team Inheritance
- Team members inherit team roles
- Team admins can manage team membership
- Teams cannot have organization participation roles

#### Role Hierarchy
- Child roles inherit from parent roles
- Ancestor roles are automatically computed
- Role membership is transitive

### Settings Affecting Permissions

- `MANAGE_ORGANIZATION_AUTH` - Allow organization admins to manage users/teams
- `ORG_ADMINS_CAN_SEE_ALL_USERS` - Allow org admins to see all users
- `ANSIBLE_BASE_ROLE_SYSTEM_ACTIVATED` - Enable ansible-base RBAC integration
- `SESSIONS_PER_USER` - Limit concurrent user sessions

### Permission Methods

#### User Methods
- `user.can_access(model, permission, obj)` - Check if user can access object
- `user.can_access_with_errors(model, permission, obj)` - Check with detailed errors
- `user.get_queryset(model, permission)` - Get filtered queryset

#### Model Methods
- `Model.access_qs(user, permission)` - Get accessible queryset
- `Model.accessible_pk_qs(user, role_field)` - Get accessible primary keys
- `Model.accessible_objects(**kwargs)` - Get accessible objects with role filtering

---

## Summary

### Key Concepts

1. **Organizations** are the top-level multi-tenancy unit
   - Contain teams, users, and resources
   - Have comprehensive role hierarchy
   - Only superusers can create

2. **Users** are the authentication and authorization entities
   - Can be members of multiple organizations and teams
   - Can have direct role assignments
   - Inherit roles through team membership

3. **Teams** provide granular user grouping within organizations
   - Belong to exactly one organization
   - Cannot be moved between organizations
   - Team members inherit team roles

4. **RBAC** provides fine-grained permissions
   - Role hierarchy with automatic inheritance
   - Direct and indirect role assignments
   - Integration with ansible-base RBAC system

### Best Practices

1. **Organization Structure**
   - Use organizations for logical business divisions
   - Assign organization admins carefully
   - Use teams for project-based groupings

2. **User Management**
   - Create users through organization admins when possible
   - Use teams to manage permissions at scale
   - Avoid direct role assignments when team membership would suffice

3. **Permission Design**
   - Leverage role hierarchy for permission inheritance
   - Use organization roles for broad permissions
   - Use resource roles for specific resource access

4. **API Usage**
   - Always check permissions before operations
   - Use related endpoints for membership management
   - Respect organization boundaries

---

*Generated from AWX codebase analysis - Comprehensive reference for Organizations, Users, and Teams functionality*
