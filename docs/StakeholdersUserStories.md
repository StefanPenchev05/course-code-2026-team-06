# 1. Requirements Elicitation

## 1.1 Stakeholders (Nicolle)

### 1.1.1 Student

- **Category:** Primary
- **Role:** Requirement provider, validator
- **Description:** Enrolled student who looks for help and requests tutoring sessions.

**Goal**
- Quickly find a tutor approved for my subject, by faculty, program and subject, at a time that fits my timetable.

**Expectation**
- Choose online or onsite, explain the purpose and what I need, and get a quick answer from the tutor.

**Concern**
- Being sure the tutor is competent, being able to cancel when my plans change, and knowing whom to contact if a session goes wrong.

### 1.1.2 Tutor

- **Category:** Primary
- **Role:** Requirement provider, validator
- **Description:** Student approved by the coordinator to tutor one or more subjects.

**Goal**
- Become a tutor through a simple application and offer the subjects I am good at.

**Expectation**
- Decide when and how I teach by setting my own free hours (online or onsite), accepting or declining requests, and knowing in advance what the student needs.

**Concern**
- Students who cancel late or don't show up, and my grades or personal data being visible to other students.

### 1.1.3 Coordinator (one per faculty)

- **Category:** Primary
- **Role:** Decision-maker, validator, domain expert
- **Description:** Staff member who approves tutors, supervises tutoring in the faculty and resolves issues.

**Goal**
- Make sure only qualified students (at least 15/20 in the subject) become tutors.

**Expectation**
- A clear overview of the faculty's tutors: schedules, number of sessions, ratings, and users who repeatedly cancel late or don't show up.

**Concern**
- Being able to act quickly when something goes wrong: all issue reports in one place, and the power to cancel sessions that break the rules or suspend users.

### 1.1.4 Lecturers

- **Category:** Secondary
- **Role:** Domain expert, requirement provider
- **Description:** Teach the courses in which students receive tutoring; they don't use the platform themselves.

**Goal**
- Students who struggle in my course get extra help.

**Expectation**
- Tutors explain the course material correctly and at the right level.

**Concern**
- Academic integrity, meaning tutors must not do graded work for students.

### 1.1.5 Sponsor (non-university)

- **Category:** Secondary
- **Role:** Requirement provider
- **Description:** Organisation that funds the platform from outside the university and receives reports on its use.

**Goal**
- Visibility, through an acknowledgement of the sponsor on the platform.

**Expectation**
- Evidence of impact, through periodic reports on active users, sessions held and feedback scores.

**Concern**
- Reports that are reliable and comparable over time, without exposing personal data.

### 1.1.6 Data Protection Officer (university)

- **Category:** External
- **Role:** Constraint source, validator
- **Description:** Ensures the platform complies with the GDPR.

**Goal**
- Personal data (grades, schedules, contact details) is collected only when it is needed.

**Expectation**
- Access depends on the role: grades are visible only to the coordinator, and each coordinator sees only their own faculty.

**Concern**
- Data leaks, and data kept longer than necessary.

### 1.1.7 Development Team (us)

- **Category:** Hidden
- **Role:** Maintainer/operator, decision-maker on technical feasibility
- **Description:** Builds, deploys and maintains the platform.

**Goal**
- Deliver a working platform within the semester and its three deliverables.

**Expectation**
- Clear, prioritized requirements that we refine with the stakeholders at every sprint.

**Concern**
- A modular, testable structure that is easy to maintain and extend, with secure handling of personal data.

---

## 1.2 User Stories

### 1.2.1 Student

1. As a student, I want to find tutors by faculty, program and subject so that I get help from someone approved for my course.
2. As a student, I want to request a session by choosing a free time, a duration, online or onsite, a purpose and a description so that the tutor knows what I need before accepting.
3. As a student, I want to be notified when a tutor accepts or declines my request so that I know whether the session will take place.
4. As a student, I want to cancel a session with a reason so that the tutor's time is freed for others.
5. As a student, I want to rate the tutor after a session so that other students and the coordinator can judge tutoring quality.

### 1.2.2 Tutor

1. As a student who wants to tutor, I want to apply with a description, my subjects and my grades so that the coordinator of my faculty can approve me.
2. As a tutor, I want to add my free time windows and mark each one as online, onsite or both so that students only request times that suit me.
3. As a tutor, I want to accept or decline session requests, giving a reason when I decline, so that I only commit to sessions I can give.
4. As a tutor, I want to cancel an accepted session with a reason so that the student is informed and can request a new one.
5. As a tutor, I want to see how many sessions and hours I have taught so that I can track my tutoring activity.

### 1.2.3 Coordinator

1. As a coordinator, I want to approve or reject tutor applications from my faculty so that only students with at least 15/20 in a subject tutor it.
2. As a coordinator, I want to see each tutor's schedule, number of sessions and ratings so that I can monitor tutoring in my faculty.
3. As a coordinator, I want to receive all issue reports in one inbox so that I can resolve problems quickly.
4. As a coordinator, I want to be alerted when a student or tutor reaches 2 late cancellations or no-shows so that I can take action.
5. As a coordinator, I want to cancel sessions that break the policy so that the platform rules are respected.
