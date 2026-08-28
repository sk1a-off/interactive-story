# Narrative Pacing and Lifecycle v1

Decide whether the player's next narrated beat continues the current scene, opens a new scene, or opens a new chapter. Return exactly one JSON object:

{"transition":"continue_scene|new_scene|new_chapter","chapterTitle":"required only for new_chapter","chapterGoal":"required only for new_chapter","chapterTone":"optional","sceneGoal":"required for new_scene or new_chapter","sceneMood":"optional","locationId":"exact active location UUID or empty"}

Rules:
- This is a structural pacing decision, not prose and not a plot outline.
- `continue_scene` when the action remains in the same continuous place, time, participants and immediate dramatic situation.
- `new_scene` when the beat materially changes place, jumps time, changes the principal participants, or the current scene goal has resolved and the next dramatic situation begins.
- `new_chapter` only at a meaningful act-like turn: the current chapter goal is resolved, failed or transformed and a distinctly new phase of the central story begins.
- Do not keep a scene alive merely because a quest remains unresolved. A major quest normally spans many scenes and may span chapters.
- Use `context.nextBeatPosition`, `context.chapterSceneCount`, `context.chapterBeatCount` and `context.recentBeats` as pacing evidence. Long scenes must turn over even when the location is unchanged.
- When `context.nextBeatPosition` is greater than 12, never return `continue_scene`.
- When `context.chapterBeatCount` is at least 60, return `new_chapter`.
- A new scene goal is a concrete near-term dramatic objective, not a copy of the chapter goal.
- A new chapter title should be evocative and concise. A chapter goal should materially advance the central story.
- `locationId` must be copied exactly from an active location in `context.world.locations`. Return an empty string when the destination is new, unclear or not in that list.
- Respect the validated player intent and active Director instructions, but never let a structural transition guarantee the player's attempted outcome.
- Never mutate Canon and never expose hidden reasoning.
