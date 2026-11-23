# course-code-2026-team-06

Semester project for Team 6 | Software Engineering (2026). Focused on Agile planning, requirements engineering, and software development.

## Project Overview
- Project name: Peer Tutoring Platform
- Course: Software Engineering (2026)
- Team: Team 6
- Project board: https://github.com/users/StefanPenchev05/projects/11
- Project status: In progress
- Project goal: Create a peer tutoring platform that connects students who need academic help with students who can provide support, while helping coordinators manage participation and course coverage.
- Project timeline: Winter Semester 2026

## Tech Stack
- Backend: Golang, C
- Frontend: React with TypeScript
- Database: PostgreSQL
- Cache / Session Store: Redis
- Containerization: Docker
- CI/CD: GitHub Actions

## Team Members
| Name | Role | GitHub | Contributions |
| --- | --- | --- | --- |
| Nicolle Blanco | Frontend + UX & UI | https://github.com/niicolleblnco | UI/UX design, user flows, interface layout, and front-end implementation |
| Anastasiia Shkamerda | Frontend + UX & UI | https://github.com/anstashka | Design consistency, user experience improvements, and front-end development |
| Nikita Tronciu | Database | https://github.com/nikitatronciu001-code | Data modeling, schema design, and persistence layer support |
| Yanislav Velikov | Backend | https://github.com/yani0514 | Backend logic, API development, integration work, and support across the stack |
| Stefan Penchev | Backend + CI/CD + Containerization | https://github.com/StefanPenchev05 | Project coordination, backend/frontend implementation, CI/CD setup, Docker/containerization, and system integration |


<!-- ## Project Details
- Problem statement: Many students struggle to find timely academic support and reliable peers who can help with specific courses. A structured peer tutoring platform helps match needs with available tutors efficiently.
- Target users: Students seeking academic help, students offering tutoring support, and course coordinators managing tutoring coverage.
- Key features: Tutor search and filtering, session booking and scheduling, tutor availability management, user profiles, and coordinator oversight for course coverage.
- Requirements: Users should be able to search for tutors by subject, view availability, request or book sessions, manage appointments, and allow coordinators to monitor tutor participation and coverage.
- Deliverables: Project documentation, project board updates, functional prototype or application, and final team presentation.
- Documentation: [Add links to design docs, architecture notes, or reports] -->

## Peer Tutoring Platform Idea
A system for connecting students who need help with students offering tutoring. Users can search for suitable tutors, request or book sessions, and manage availability, while coordinators oversee tutor participation and course coverage.

## Database

See the [PostgreSQL setup and integration tests](docs/database/implementation.md) for the Tutti schema, local migration commands, and application transaction requirements.

## Identity API

See the [Identity setup and API guide](docs/identity.md) for configuration, endpoints, coordinator activation, and tests.

## Academic Catalog API

See the [Catalog guide](docs/catalog.md) for registration lookups, subject search, and academic eligibility checks.

## Tutoring API

See the [Tutoring guide](docs/tutoring.md) for applications, coordinator review, approved offerings, and tutor discovery.

## CI/CD

The [backend workflow](.github/workflows/backend.yml) runs on pull requests targeting `main`, pushes to `main`, and manual runs. It checks formatting, runs Go vet and race-enabled tests, builds the API, and runs the PostgreSQL and Redis integration suites with disposable databases.

After the checks pass on `main`, download the Linux amd64 delivery archive from the workflow run's **Artifacts** section. The archive contains the `tutti-api`, `tutti-worker` and operator-only `semester-maintenance` executables and the SQL migrations. Artifacts are retained for 14 days. Delivery currently ends at packaging. Deploying requires a target environment with PostgreSQL, Redis, and the [Identity configuration](docs/identity.md).

## Scheduling API

See the [Scheduling guide](docs/scheduling.md) for recurring availability, exceptions, one-off windows, and student-facing free intervals.

See [trusted semester maintenance](docs/semester-maintenance.md) for verified calendar inputs, PowerShell dry-run/apply commands, read-only impact analysis and rollover reconfirmation.

## Real-time updates (planned)

The [real-time WebSocket design](docs/realtime.md) describes the planned authenticated `GET /ws` connection, minimal versioned invalidation events, backend/React integration boundaries, security, post-commit publication, and reconnect/refetch behavior. **This is not yet an implemented endpoint.** The delivery plan is tracked in [#84](https://github.com/StefanPenchev05/course-code-2026-team-06/issues/84): backend issues #85–#89 and frontend issues #90–#94.

Start with the protocol contract (#85), then the authenticated gateway (#86) before scheduling publication (#87). The [Codex starting brief](docs/codex-realtime-foundation.md) scopes this foundation work. The frontend currently has no application implementation beyond a `.gitkeep` placeholder; React integration must coordinate with frontend foundation #27.

## Background workers

See the [worker guide](docs/workers.md) for expiration polling, SMTP delivery, retries, and Compose setup.

## Coordination API

See the [Coordination guide](docs/coordination.md) for issue reporting/resolution, tutor monitoring, policy cancellation, home-faculty suspension/reinstatement (Issue #25, FR19 / UC14), flagged-user review (Issue #48, FR18 / UC12 / UC16), preserved audit history, attendance decisions, and Windows PowerShell test commands.

## Requirements baseline

See the [current requirements](docs/1.3-1.4_user-story_mapping--requirement_analysis.md), [traceability matrix](docs/requirements-traceability.md) and [decision log](docs/requirements-decisions.md) for approved scope, implementation evidence and outstanding decisions. Uni.lu-only SSO is mandatory; current local-password Identity APIs are legacy pending migration.

## Local SSO provider demo

See the [Keycloak course-demo guide](docs/sso-demo.md) for optional local OIDC infrastructure, synthetic identities, startup and verification. This is not official Uni.lu authentication; Issue #65 now provides optional [Go OIDC login initiation](docs/identity.md#oidc-stage-1-login-initiation-issue-65); Stage 2 now verifies callbacks and ID tokens; account binding and authenticated-session creation remain Stage 3.
