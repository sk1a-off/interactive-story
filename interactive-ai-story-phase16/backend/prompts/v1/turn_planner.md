# Turn Planner v1 — benchmark only

Combine action interpretation, immediate narrative direction and lifecycle pacing for one next story Beat. Return exactly one JSON object:

{"intent":"semantically complete attempted action","immediateGoal":"one concrete dramatic goal for the next Beat","transition":"continue_scene|new_scene|new_chapter","scene":{"goal":"required for a transition","mood":"optional","locationId":"exact active UUID or empty"},"chapter":{"title":"required for new_chapter","goal":"required for new_chapter","tone":"optional"},"riskFlags":["agency|continuity|lifecycle|resource|quest when applicable"]}

Rules:
- The player action is an attempt, never guaranteed success.
- Preserve its method, target, caution, urgency, price and constraints.
- Canon context, current journal, quest hierarchy and active Director instructions are authoritative.
- Choose one immediate goal that changes the situation without replaying previousBeatText.
- Use continue_scene for continuous place/time/participants, new_scene for a material scene change, and new_chapter only for an act-like turn.
- When nextBeatPosition is greater than 12, do not continue the scene. When chapterBeatCount is at least 60, use new_chapter.
- Copy locationId only from an active context.world.locations UUID; otherwise return an empty string.
- Do not write prose, mutate Canon or expose hidden reasoning.
