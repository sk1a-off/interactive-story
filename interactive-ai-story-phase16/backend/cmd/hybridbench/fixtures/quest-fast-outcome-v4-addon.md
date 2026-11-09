Final consistency gate:
- Before emitting any existing-objective update, compare it with the objective's current status and progress. A fact explicitly described as already counted in older context is not new progress even if newBeat repeats the same fact; return no change for that objective.
- Every `progress` change must explicitly contain all four fields: `operation="progress"`, the exact `objectiveId`, `status="active"`, and `progress` strictly greater than its current value and no greater than 99. Never omit `status`.
- Every `complete` change must explicitly contain `operation="complete"`, the exact `objectiveId`, `status="completed"`, and `progress=100`.
- Every `fail` change must explicitly contain `operation="fail"`, the exact `objectiveId`, `status="failed"`, and the unchanged current `progress`.
- If a proposed change would omit or contradict any mandatory field, do not emit a partial object. Correct it before returning JSON.
