# Project Architecture

This document defines the proposed file structure for the **Peer Tutoring Platform**, Team 6's Software Engineering project for Winter Semester 2026. The project uses Domain-Driven Design (DDD) with a modular monolith: one Go backend organized into business modules, one React frontend, PostgreSQL for persistent data, and Redis for cache and sessions.

This is an implementation blueprint. The directories below are planned and should be created as their features are implemented. Project and team information lives in the repository's root README.

## Technology Stack

| Area | Technology | Responsibility |
| --- | --- | --- |
| Backend | Go | HTTP API, application workflows, and domain rules |
| Native backend components | C | Isolated native functionality when its specific purpose is agreed |
| Frontend | React with TypeScript | Student, tutor, and coordinator interfaces |
| Database | PostgreSQL | Authoritative application data |
| Cache / Session Store | Redis | Temporary cached data and authentication sessions |
| Containerization | Docker | Consistent application and service environments |
| CI/CD | GitHub Actions | Automated checks, builds, and deployment workflows |

## Planned Repository Structure

```text
course-code-2026-team-06/
├── .github/
│   └── workflows/
│       ├── backend-ci.yml          # Go checks, tests, and build
│       ├── frontend-ci.yml         # TypeScript checks, tests, and build
│       └── containers-ci.yml       # Docker image builds
├── backend/
│   ├── cmd/
│   │   └── api/
│   │       └── main.go             # Start the HTTP server
│   ├── internal/
│   │   ├── bootstrap/
│   │   │   ├── app.go              # Wire modules, adapters, and dependencies
│   │   │   └── routes.go           # Register module HTTP routes
│   │   ├── identity/              # Accounts, roles, authentication
│   │   ├── catalog/               # Courses and subjects
│   │   ├── tutoring/              # Tutor profiles, offerings, approval
│   │   ├── scheduling/            # Availability and session bookings
│   │   ├── coordination/          # Participation and course coverage views
│   │   └── platform/
│   │       ├── config/            # Environment-based configuration
│   │       ├── postgres/          # Database connection setup
│   │       ├── redis/             # Redis connection setup
│   │       ├── httpserver/        # Server setup and common middleware
│   │       └── logging/           # Logging setup
│   ├── migrations/               # Ordered PostgreSQL up/down SQL migrations
│   ├── native/                   # Optional C source; purpose still to be decided
│   │   ├── include/              # C headers
│   │   ├── src/                  # C implementations
│   │   ├── tests/                # Native component tests
│   │   └── Makefile              # Native build commands
│   ├── tests/
│   │   └── integration/          # Tests using real PostgreSQL and Redis
│   ├── go.mod
│   ├── go.sum
│   └── Dockerfile
├── frontend/
│   ├── public/                   # Static assets
│   ├── src/
│   │   ├── app/
│   │   │   ├── App.tsx           # Application root
│   │   │   ├── router.tsx        # Page routes
│   │   │   └── providers.tsx     # Shared application providers
│   │   ├── pages/                # Route-level screens composed from features
│   │   ├── features/
│   │   │   ├── auth/
│   │   │   ├── courses/
│   │   │   ├── tutors/
│   │   │   ├── availability/
│   │   │   ├── bookings/
│   │   │   └── coordination/
│   │   ├── shared/
│   │   │   ├── api/              # HTTP client and API contract types
│   │   │   ├── components/       # Reusable interface components
│   │   │   ├── hooks/            # Reusable UI hooks
│   │   │   └── utils/            # General frontend utilities
│   │   ├── styles/               # Global styles and design tokens
│   │   └── main.tsx              # React entry point
│   ├── tests/
│   │   └── e2e/                  # Complete user journey tests
│   ├── package.json
│   ├── tsconfig.json
│   └── Dockerfile
├── api/
│   └── openapi.yaml             # HTTP API contract
├── docs/
│   ├── README.md                # This architecture guide
│   ├── requirements.md          # Functional and nonfunctional requirements
│   ├── domain-model.md          # Business terminology, entities, and boundaries
│   ├── database.md              # Schema, relationships, and persistence decisions
│   ├── development.md           # Local setup and development commands
│   └── decisions/               # Architecture decision records
├── scripts/                    # Setup and development helper scripts
├── compose.yaml                # Backend, frontend, PostgreSQL, and Redis services
├── .env.example                # Documented configuration placeholders
├── .gitignore
├── README.md                   # Project overview, team, and entry points
└── LICENSE
```

## DDD Modules and Ownership

The module boundaries below are an initial domain model, to be refined during requirements engineering. User roles do not each get a separate backend: students, tutors, and coordinators use the same business modules with different permissions.

| Module | Owns | Example responsibilities |
| --- | --- | --- |
| `identity` | User accounts and role assignments | Sign in, manage account details, assign permissions |
| `catalog` | Courses and subjects | List courses and maintain course information |
| `tutoring` | Tutor profiles, course offerings, and tutor approval status | Find suitable tutors, list supported courses, approve participation |
| `scheduling` | Tutor availability and bookings | Publish availability, request a session, confirm, decline, or cancel a booking |
| `coordination` | Reporting queries and read models | Show tutor participation and identify gaps in course coverage |

Coordination initially serves as a reporting module. It uses defined query interfaces to obtain data from the owning modules. Approval actions belong to `tutoring`; account role changes belong to `identity`.

## Structure Within a Backend Module

Each business module follows the same dependency structure. The scheduling module illustrates the pattern:

```text
backend/internal/scheduling/
├── domain/
│   ├── booking.go               # Booking entity and allowed state transitions
│   ├── availability.go          # Tutor availability rules
│   ├── time_range.go            # Validated time-range value object
│   ├── repository.go            # Persistence interfaces for domain objects
│   ├── errors.go                # Business errors
│   └── booking_test.go          # Domain rule tests
├── application/
│   ├── request_booking.go       # Request a session use case
│   ├── confirm_booking.go       # Confirm a session use case
│   ├── cancel_booking.go        # Cancel a session use case
│   ├── manage_availability.go   # Update tutor availability
│   ├── list_bookings.go         # Booking query use case
│   ├── ports.go                 # Tutor eligibility, clock, and transaction interfaces
│   └── request_booking_test.go  # Use case tests with test doubles
├── infrastructure/
│   ├── postgres/
│   │   ├── booking_repository.go
│   │   └── availability_repository.go
│   └── tutoring/
│       └── eligibility_adapter.go # Calls tutoring's published application interface
└── interfaces/
    └── http/
        ├── handler.go          # Decode requests and invoke use cases
        ├── routes.go           # Module endpoints
        └── dto.go              # HTTP request and response shapes
```

| Layer | Responsibility | Dependency rule |
| --- | --- | --- |
| Domain | Entities, value objects, invariants, and repository interfaces | Independent of HTTP, databases, Redis, and other modules' implementation details |
| Application | Execute use cases, check authorization, coordinate transactions | Depends on its domain and interfaces it needs |
| Infrastructure | Implement persistence and external integrations | Implements domain/application interfaces |
| Interfaces | Translate HTTP input and output | Calls application use cases |
| Bootstrap | Construct and connect concrete implementations | Wires all layers at startup |

Runtime flow is HTTP handler → application use case → domain behavior and repository interface → PostgreSQL adapter. Source dependencies point toward the domain and application interfaces; business code does not import concrete database adapters.

Keep business rules inside domain objects or domain services. For example, a booking defines which status changes are valid; the application use case verifies the caller's permissions and saves the result through a repository.

## Integration and Data Rules

- Each module owns its data and writes through its own application workflows. Do not directly use another module's tables or repository implementations.
- Communicate across modules through explicit application interfaces. Refer to another module's entities by identifier rather than sharing mutable domain objects.
- Keep availability and bookings in `scheduling` so reserving a time can be handled atomically. Use PostgreSQL transactions and constraints or locking to prevent concurrent requests from booking the same capacity; an application-only availability check is insufficient.
- PostgreSQL stores durable accounts, courses, tutor offerings, availability, and bookings. Redis stores authentication sessions and optional cached query results; cache loss must not erase bookings or other durable data.
- Place module-specific SQL in that module's PostgreSQL adapter. Keep schema migrations in a single ordered `backend/migrations/` directory, with filenames identifying the affected module.
- Keep `platform` limited to technical setup. Business rules belong to their owning module.
- The role of C is still open. Once defined, isolate its source under `backend/native/` and access it through an infrastructure adapter behind an application interface. Record the integration choice, such as cgo or a separate process, in an architecture decision before implementation.

## Frontend Organization

Organize React code by user-facing feature. Each feature can contain `components/`, `hooks/`, `api/`, and `types/` as needed, with component tests alongside the implementation. Pages compose features into screens; `shared` contains reusable UI and technical utilities.

The frontend uses the HTTP API contract rather than backend domain structs. It can validate input for immediate feedback, but the backend remains responsible for authorization and business invariants.

## Testing and Delivery

- Keep Go unit tests beside the code they verify using `*_test.go` files.
- Use backend integration tests to verify migrations, persistence, session storage, and concurrent booking behavior against PostgreSQL and Redis.
- Test frontend components alongside their source and keep end-to-end journeys in `frontend/tests/e2e/`, including finding a tutor, requesting a session, and confirming the booking.
- Use Docker Compose for the local application and its services. Keep real secrets outside version control and document required variables in `.env.example`.
- Use GitHub Actions to check formatting, types, tests, and builds. Add native build checks when C code is introduced. Add a deployment workflow once the hosting target is chosen.
