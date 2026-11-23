# 9. GenAI Experience

## 9.1. Objective and methodology

We used generative AI to review artifacts our group had already produced in the first stage. The specification, the user stories, the acceptance criteria and the test matrix were written by us before the experiments. The AI came in afterwards as a reviewer, to point out gaps. The decisions stayed with our group.

We compared two tools, **ChatGPT** (model GPT 5.6-Luna) and **Google Gemini** (model hydrogen), both on the free tier. Each one received two tasks with equivalent instructions:

1. **Requirements review:** point out up to five relevant problems, without rewriting the requirements, giving the requirement, the problem, the severity and a suggestion.
2. **Test coverage review:** point out up to five missing or poorly covered scenarios, without repeating existing cases or inventing business rules, giving the requirement, the scenario, the expected result and the priority.

The context provided was the first-stage specification (*Stage 1*), with functional and non-functional requirements, business rules, acceptance criteria, UML and planned test cases. We then checked every answer against the document itself. This check was necessary: the answers contained repeated suggestions, debatable interpretations and at least one false positive.

**How we arrived at the prompts.** Our first versions were long and detailed. We switched to short prompts that ask for short, to-the-point answers, limit the number of items and forbid inventing problems or rules just to fill the list, precisely to reduce hallucination. The final prompts are in Appendix A.

## 9.2. Experiment 1: requirements review

Same prompt and same answer format in both tools. Each returned five items, which add up to eight distinct findings because some of them coincided.

| AI finding | Model | What we found in the specification | Decision |
|---|---|---|---|
| Validation of external URLs is poorly defined (FR-W-02). | ChatGPT | Valid, and already listed as an open decision in section 9.1 of Stage 1 (allowed URL list). | We keep it open. The domain list the AI suggested is a solution idea, not a rule we approved. |
| Normalization for detecting duplicates (FR-W-07, BR-01). | ChatGPT and Gemini | Valid. TW-17 covers uppercase/lowercase and extra spaces, but accents, punctuation and spelling of author names are not defined. | We will define the normalization. Gemini's suggestion to limit the check to certain lifecycle states was not accepted, because the specification has no such policy and the decision is ours. |
| Accepted range for the Year field. | ChatGPT | Valid, but we already knew: section 9.1 of Stage 1 marks it as "range policy TBD". | Stays open. It does not count as a discovery by the AI. |
| Rules for editing published records (FR-W-04). | ChatGPT and Gemini | Valid as a policy question. UC-02 (A2) and D5 keep the record published after an edit, but do not say which validations run again. | Left as a point to clarify. We did not adopt the suggestion to require new moderation on every edit, because that is a product decision. |
| Interaction between text search and filters (FR-S-01 to FR-S-04). | ChatGPT | Partly valid. Fields and AND combination are defined, but how the text is matched (normalization) is not detailed. | We only took the point about text matching. We rejected the criticism that the filters were not specified. |
| Institutional contacts with no attribute or flow to record the permission (FR-S-08, NFR-04, BR-07). | Gemini | Valid. The specification makes display conditional on permission, but the class diagram has no consent attribute and no flow records that authorization. | Our gap. We still need to define how the permission is recorded and checked, and write an acceptance criterion and a test. |
| Contradiction in the number of advisors (BR-04 and class diagram). | Gemini | **False positive.** BR-04 requires an advisor at submission, the MVP stores one primary advisor (section 9.1 of Stage 1), and the 0..1 in the diagram allows a draft without an advisor. | Rejected. We keep 0..1. Gemini did not take the note in section 9.1 of Stage 1 into account. |
| No tie-breaking criterion for similar works (FR-S-09). | Gemini | Valid. The requirement orders by keyword overlap, without saying what to do on ties. | Low-priority improvement, noted. |

**Comparison between the tools.** Both converged on two points: duplicate normalization and editing of published records. ChatGPT also raised external URLs, the year range and search semantics, and much of that was already recorded or partly covered. Gemini found two real gaps we had not seen (consent for contacts and tie-breaking), but was wrong when it claimed a contradiction about advisors. Five items each does not mean the same quality: among the eight findings, one was a false positive, two were already recorded as open decisions and one was only partly correct.

## 9.3. Experiment 2: test coverage review

Both tools received the test matrix together with the requirements and the acceptance criteria. The prompt asked for up to five missing or poorly covered scenarios, forbade repeating existing tests and required an expected result consistent with the documented rules.

We checked each suggestion against TS-01 to TS-12 and TW-01 to TW-27 before saying a scenario was missing.

| Suggested scenario | Model | What the matrix already has | Decision |
|---|---|---|---|
| Show contacts only with explicit permission (FR-S-08, NFR-04, BR-07). | ChatGPT and Gemini | No test covers the display of email or office data. | Accepted. It comes directly from the privacy requirements and is the counterpart of the gap in Experiment 1. |
| Only PUBLISHED records appear in search, including UNDER_REVIEW and REJECTED. | ChatGPT and Gemini | TS-08 only tests DRAFT and PUBLISHED. | Accepted as an extension of TS-08. Direct access by URL depends on the details-page policy, which we have not defined yet. |
| Moderation by someone who is not a COORDINATOR, and anonymous writes. | ChatGPT and Gemini | TW-19 only uses RESEARCHER. A LIBRARY_WORKER trying to moderate and the anonymous case are missing. | Accepted. It exercises BR-03, NFR-03 and AC-04c. |
| One valid source and one invalid source (invalid PDF with a valid URL, or the reverse). | ChatGPT | TW-10 to TW-15 cover each source alone or both valid, but not the mix. | Accepted, with the expected result to be confirmed. By the "at least one valid source" rule it should pass, but we have not defined what to do with the extra invalid source. |
| File with a .pdf extension whose content is not a PDF. | ChatGPT | TW-14 only rejects DOCX. | Accepted. It ensures the NFR-03 validation looks at the content and not only the extension. |
| Details of a published work with no PDF and no URL. | Gemini | TS-09 only tests with a valid URL. AC-02c provides for the empty state, but there is no case in the matrix. | Accepted. Expected: a clear "unavailable" indication, with no broken link (AC-02c, FR-S-07). |
| Filters by author, advisor and document type. | Gemini | TS-02 only combines year and area. | Accepted. FR-S-02 and FR-S-03 need their own cases and the AND combination. |

**Comparison between the tools.** This time the gaps were complementary. Both pointed out contact privacy, visibility of lifecycle states and authorization in moderation. ChatGPT added two scenarios on file and source validation. Gemini added the detail page without a document and the untested filters. We accepted all seven suggestions, but several are extensions of existing tests rather than independent cases. The accepted scenarios have not yet received IDs in the matrix.

## 9.4. AI use across the development lifecycle

| SDLC phase | Activity | AI use | Contribution and team role |
|---|---|---|---|
| Requirements | Review of requirements and criteria | ChatGPT and Gemini reviewed requirements that were already finished (Experiment 1). | They widened the list of points to check. We checked each finding against the document, and the incorrect or redundant ones were discarded. |
| Testing | Review of the test matrix | ChatGPT and Gemini (Experiment 2). | They pointed out seven coverage gaps, all accepted. We checked them against the matrix to avoid duplicate cases. |
| Modelling | UML diagrams in PlantUML | AI suggestions while writing and reviewing the diagrams. | They served as support. We chose the models and checked consistency with the requirements and use cases. |
| Specify and Clarify (SDD) | Specification as source of truth | Claude Opus 5.5 received the specification (spec.pdf) with the instruction to read it, build the prototype and ask questions before proceeding if in doubt. | The agent asked few questions, almost all about implementation details, which we answered. |
| Plan and Tasks (SDD) | Stack and decomposition | The prototype was organized into four modules: Domain, App, Web and Store. | We defined Go, SQLite and HTML templates. We assessed the modules against the package diagram (Domain, App, Web and Store correspond roughly to the domain, application, presentation and infrastructure layers). |
| Implement (SDD) | Go/SQLite prototype and tests | Claude generated the prototype and tests for domain logic, features, requirements and acceptance criteria. | We ran the prototype and compared the flows with the specification and the UML sequences. |
| Analyze/Converge | Consistency between specification and prototype | Done by us, based on the generated prototype. | We considered the prototype very close to the specified behaviour. The assessment was qualitative: this report does not include a count of executed tests or measured coverage. The 39 cases in the Stage 1 test section are planned and must not be confused with tests run on the prototype. |

## 9.5. Critical reflection

**Did the AI improve the artifacts or just produce them faster?** In the two reviews, it improved them. At least four findings changed what we will do: consent for contacts, tie-breaking for similar works, filter tests and validation of PDF content. We had not seen any of them on our own. The prototype is a different case: the AI generated it, and our contribution was to check whether its behaviour matched the specification.

**Group versus AI.** What the AI found and we did not: the four points above. What the AI got wrong and we got right: the multiplicity of advisors. The false alarms and the already-known findings show that the AI does not tell a deliberate scope choice from an inconsistency very well, and sometimes treats an optional improvement as a mandatory requirement. It also rated as serious some things that were not defects.

**What depended on us.** Checking each finding in the document, interpreting the MVP decisions and deciding what to keep. Policies such as requiring new moderation after an edit, the states considered in the duplicate check and the allowed external domains cannot come from the AI, because they depend on what we want from the system.

**Prompts.** Short prompts, with an item limit and an explicit ban on inventing, were what we adopted as the final version. Short answers were easier to check against the specification, but the five-item limit may also have left findings out, and we did not test that.

**SDD.** Having the specification as the source of truth gave the prototype a clear reference and a scope limit. Generating code and tests, on the other hand, does not prove that all requirements were met. To claim that, we would need a requirement-by-requirement mapping with execution results.

In short, the AI widened what we were able to see in the artifacts, but the value came from deciding which findings were real.

---

## Appendix A: prompts used (final version)

Earlier prompt versions produced very long answers, the answers hallucinated a lot and took longer to review, so we discarded them. The prompts below produce shorter, more direct answers and are the ones we adopted as the final version. They were written in Portuguese and are reproduced here with an English translation.

### A.1. Requirements review

> Analyze the requirements provided and identify only the most relevant problems. Identify at most five problems; do not rewrite the requirements; do not explain theoretical concepts; do not write long introductions or conclusions; limit each problem to three sentences; do not invent problems to fill the list. For each problem, give the requirement, the problem identified, the severity and a suggestion. At the end, state the number of problems found, how many you consider truly relevant and a note on the main limitation of the analysis. Prioritize problems that could cause implementation errors, ambiguities or failures in the acceptance criteria.

### A.2. Test coverage review

> Analyze the requirements and the test cases provided. Identify only the five most important test cases that are missing or insufficiently covered. Do not repeat existing tests, do not invent business rules, do not generate redundant tests and do not write long explanations. If there are fewer than five relevant gaps, present only those found. For each scenario, give the related requirement, the uncovered scenario, the expected result and the priority, limiting the description to two sentences. If you find no relevant gaps, say so explicitly.
