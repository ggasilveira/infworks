# Spec-Driven Development

For implementing the system prototype, we used spec-driven development
to build an application in Go and SQLite with Claude Opus 5.5, using
the specification document our team made as the source of truth.

## Specify

The specification document specifies what needs to be built in the "Scope
and Functionalities" section. By stating exactly the two features to focus on,
and the responsibilities of the system, the agent had clear guidelines to follow,
and avoided diverging on things we declared as "out of scope".

## Clarify

The used prompt was:

```
Read the spec.pdf file and build a prototype according to the specification.
If you have any questions, ask me before proceeding.
```

By telling the agent to clear any questions before implementing the system,
we were able to identify gaps in the specification, as well as direct the
agent with close precision; this way, decisions about stack, architecture
and implementation not defined by the specification remained under our control.

The agent ended up asking only a few questions, mostly related to the implementation,
which showed the specification was fairly complete, with almost no ambiguity.

## Plan

The chosen stack for the prototype was Go/SQLite, using HTML templates for the
front-end. Go was chosen for its simplicity and ready-made solutions in the
standard library and dependencies, making it suitable for a prototype.

## Tasks

The prototype was divided into 4 modules:

- Domain: business rules of the application
- App: code implementing the system functionality and features
- Web: handlers for the requests and front-end with html templates
- Store: data model and database driver

## Implement

Claude built the system and generated test cases also from the specification,
including tests for the domain logic, features, acceptance criteria and
functional requirements.

## Analysis/Coverage

The prototype was tested by the team and concluded to be very close to what
was proposed in the specification document. The two functionalities -
search/discovery and cataloguing - followed all functional requirements,
business rules and acceptance criteria. The UI/UX flows for the functionalities
also followed the sequence diagrams in the specification.
