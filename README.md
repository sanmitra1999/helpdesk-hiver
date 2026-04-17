# Helpdesk Ticket Assignment Service (Go)

A backend-only helpdesk service in Go that manages agents, accepts tickets, assigns them automatically, and exposes APIs to inspect routing state.

## What is included

- Agent management
- Ticket creation, resolution, and reopening
- Automatic assignment based on skill, language preference, shift, online status, and capacity
- Assignment history with human-readable reasons
- Docker Compose setup with PostgreSQL database
- Persistent storage using PostgreSQL database

## Run

```bash
docker compose up --build
```

Service URL:

```bash
http://localhost:8080
```

Health check:

```bash
curl http://localhost:8080/healthz
```

## API list

```text
POST   /agents
PATCH  /agents/:id/status
GET    /agents/:id/tickets

POST   /tickets
PATCH  /tickets/:id/resolve
PATCH  /tickets/:id/reopen
GET    /tickets/:id

GET    /assignments/summary
```

## Sample curl commands

### 1) Create agent

```bash
curl -X POST http://localhost:8080/agents \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Asha",
    "email": "asha@example.com",
    "skills": ["billing", "general"],
    "languages": ["english", "hindi"],
    "shift_start_utc": "00:00",
    "shift_end_utc": "23:59",
    "max_capacity": 2,
    "is_online": true
  }'
```

### 2) Create another agent

```bash
curl -X POST http://localhost:8080/agents \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Marc",
    "email": "marc@example.com",
    "skills": ["technical"],
    "languages": ["english", "french"],
    "shift_start_utc": "00:00",
    "shift_end_utc": "23:59",
    "max_capacity": 1,
    "is_online": true
  }'
```

### 3) Create ticket

```bash
curl -X POST http://localhost:8080/tickets \
  -H "Content-Type: application/json" \
  -d '{
    "customer_name": "Ravi",
    "customer_email": "ravi@example.com",
    "category": "billing",
    "language_preference": "hindi",
    "priority": "high"
  }'
```

### 4) Resolve ticket

```bash
curl -X PATCH http://localhost:8080/tickets/1/resolve
```

### 5) Reopen ticket

```bash
curl -X PATCH http://localhost:8080/tickets/1/reopen
```

### 6) Toggle agent offline

```bash
curl -X PATCH http://localhost:8080/agents/1/status \
  -H "Content-Type: application/json" \
  -d '{"online": false}'
```

### 7) Agent open tickets

```bash
curl http://localhost:8080/agents/1/tickets
```

### 8) Ticket detail + assignment history

```bash
curl http://localhost:8080/tickets/1
```

### 9) Workload summary

```bash
curl http://localhost:8080/assignments/summary
```

## Data model

### Agent
- name
- email
- skills[]
- languages[]
- shift_start_utc / shift_end_utc
- max_capacity
- is_online
- created_at / updated_at

### Ticket
- customer_name
- customer_email
- category
- language_preference
- priority
- status
- current_agent_id
- created_at / assigned_at / resolved_at / updated_at

### Assignment history
- ticket_id
- agent_id (nullable for pending states)
- event_type
- reason
- created_at

## Assignment logic implemented

1. Agent must be online.
2. Agent must be inside the current UTC shift window.
3. Agent must have the required skill.
4. Agent must have free capacity.
5. If any eligible agents match the preferred language, the system considers only that subset.
6. Inside that subset, choose the lowest current load.
7. Tie-breaker is lower agent id.

## Ambiguities and decisions

### What if no eligible agent exists?
The ticket stays in the queue as `unassigned` when first created, or `reopened` when reopened. A `pending` assignment-history event is added with the reason.

### What happens when an agent goes offline?
All currently assigned open tickets for that agent are unassigned immediately and pushed back into the queue. The system then tries to reassign them to other eligible agents.

### What happens when a ticket is reopened?
The system first tries to return the ticket to the same agent who last handled it, as long as that agent is still online, in shift, skilled, and has capacity. If not, the ticket falls back to the normal routing flow.

### How are shifts interpreted?
All shifts are UTC and use `HH:MM` format. Overnight shifts are supported. Example: `22:00` to `06:00`. If start and end are equal, that is treated as a 24-hour shift.

### Is priority used in assignment?
Priority does not override the required skill/capacity rules, but it does control queue order. Pending tickets are retried in this order: `urgent`, `high`, `medium`, `low`.


## Schema design choices and why

- Arrays for `skills` and `languages`: simple for this exercise and enough for exact-match routing.
- `current_agent_id` on ticket: makes current ownership cheap to read.
- Separate assignment history log: preserves all routing decisions and reasons over time.

## What I would do differently with more time

- Move persistence to PostgreSQL with migrations.
- Add background worker / retry scheduler instead of only retrying on state changes.
- Add integration tests for shift edge cases, reopen flows, and concurrent assignment under real PostgreSQL load.
- Reduce lock scope further for higher throughput, for example with queue-claiming patterns such as `SKIP LOCKED`.
- Add metrics and structured logs.
- Add pagination and filtering for ticket history APIs.

## Capacity definition:

- Capacity is checked against tickets currently in assigned status for that agent at that moment.
- When a ticket is resolved or unassigned, that slot is immediately freed.
- There is no per-shift assignment quota in this model.
- New assignments are blocked only when current open assigned tickets reach max capacity.
Why
- It matches real-time workload control and keeps routing simple.
- It avoids artificial limits where an agent could be idle later in the shift but still blocked.

## Concurrency handling

- Assignment attempts run inside a PostgreSQL transaction.
- The ticket row is locked with `FOR UPDATE` before assignment so the same ticket cannot be assigned twice concurrently.
- Candidate agent rows are locked before checking capacity, shift, and skill eligibility.
- The ticket update and assignment-history insert happen in the same transaction and commit together.
- This uses pessimistic locking to keep capacity enforcement correct under concurrent requests.


## Priority handling

- A high priority ticket does not take over capacity from an already-assigned low priority ticket.
- If all eligible agents are full, the high priority ticket goes to the queue and is processed before lower-priority queued tickets.
- Already-assigned tickets keep ownership until resolved, reopened, or explicitly re-queued by an operational event (for example agent offline).
- Preemption causes customer context loss and assignment churn.
- Capacity remains predictable and fair.
- Priority still matters strongly through queue ordering and first-eligible assignment when capacity frees up.


## Rebalancing

- When a new agent comes online, existing unassigned/reopened tickets should auto-route to them.
- Already-assigned tickets should not be rebalanced just because a new agent comes online.
- It preserves ownership/context for active tickets.
- It avoids churn and customer handoff noise.
- It still improves latency for queued work when new capacity appears.


## Overflow queue:

- If all agents are at capacity, the ticket is kept in the overflow queue as unassigned (or reopened for reopened cases).
- The system does not force-assign beyond capacity.
- The system does not reject the ticket at intake just because capacity is full.
- A pending audit event is recorded, and the ticket is retried when capacity becomes available (agent goes online, ticket resolved, etc.).
- Rationale: this preserves SLA fairness and auditability, avoids overloading agents, and ensures no customer ticket is dropped.


## Shift boundary:
- If an agent’s shift ends while they still have open tickets, those tickets stay assigned to that agent.
- The agent is not eligible for any new ticket assignments once out of shift.
- Any newly pending tickets are routed only to agents who are online and currently within shift.
- If you later mark that agent offline, their assigned tickets are unassigned and re-queued as normal.
Why:
- Preserves conversation context and ownership for already-handled customers.
- Prevents assignment churn at shift boundaries.
- Keeps capacity rules strict for new work while allowing graceful completion of in-flight tickets.





