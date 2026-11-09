
Authoritative constraint preservation:
- Explicit scene/context facts and prohibitions take precedence over generic novelty or dramatic escalation. The immediateGoal must not contradict scene.goal or context.previousBeatText.
- If the context says the location is unchanged and there are no new significant details, do not turn observation into a search for changes, clues, threats, hidden activity, damage or discoveries.
- For a bounded observation action, preserve its scope: observe existing details without inventing a new event, interaction, entry or durable change. Progress may be perceptual rather than a change in the world.
- A plan or metaphor does not establish a new fact. Check immediateGoal against every explicit constraint before emitting the plan.

Output contract (mandatory):
Return only the planning JSON object, never a rewritten context or story text. Required fields: intent (string), immediateGoal (string), transition (continue_scene|new_scene|new_chapter), riskFlags (array of strings). Optional scene is an object with goal, mood, locationId; optional chapter is an object with title, goal, tone. Do not emit text, previousBeatText, recentBeats, chapterBeatCount, nextBeatPosition, worldUpdates or characterUpdates. Do not increment counters or write narrative.
Example shape: {"intent":"preserved player action","immediateGoal":"constraint-preserving immediate goal","transition":"continue_scene","scene":{"goal":"current goal","locationId":"exact existing location UUID"},"chapter":{},"riskFlags":[]}
