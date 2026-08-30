# Canon objectives

Objectives are timeline-local, replayable Story Canon—not browser state. They use the existing `story_threads` projection with `metadata.kind = "objective"` and two semantic events: `objective_created` and `objective_updated`.

## Lifecycle

- `global`: a long-running story aim, initially derived from `opening_situation.chapterGoal`;
- `minor`: a concrete near-term task, initially derived from `opening_situation.sceneGoal`;
- `active`: progress may grow monotonically from 0 to 99;
- `completed`: progress is 100 and concrete evidence is required;
- `failed`: concrete evidence is required; the row is retained for history.

Older timelines expose their existing chapter/scene goals as virtual objectives. On the next generated turn the Objective Evaluator canonizes them. New timelines create objective rows and `objective_created` events during `StartStory`.

## Generation order

`Action Interpreter → Director → Writer → Objective Evaluator → Choices → validation → atomic Canon commit`

The Director and Writer receive active objectives as constraints. The Choices role receives both current objectives and validated changes from the new beat, so offered actions can advance goals without removing player agency. The evaluator sees the newly written beat and may create, progress, complete, or fail objectives. A selected action is only an attempt: completion is rejected unless the evaluator supplies evidence from the resulting beat.

All objective changes are server-validated (known UUID for updates, valid scope/status, monotonic progress, evidence for terminal transitions, maximum five changes) before any event is appended. A malformed objective proposal leaves the entire turn outside Canon.

## Reader and Director

Reader shows active global/minor objectives in a sticky left journal with progress and collapsible terminal history. The journal can collapse to a counter rail and remembers that preference per Timeline. Below 960px it becomes an off-canvas drawer with a persistent floating trigger, backdrop and `Escape` close behavior. Director shows the same projection together with success criteria and evidence. API collections are always arrays; the frontend also tolerates legacy `null` values.

Forks clone objective threads with fresh timeline-local IDs. Replay reconstructs them from semantic events, so progress cannot leak between branches.
