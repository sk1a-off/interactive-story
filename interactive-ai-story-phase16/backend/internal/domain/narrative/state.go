package narrative

import "github.com/local/interactive-ai-story/backend/internal/domain/id"

type State struct {
	TimelineID           id.ID
	Chapters             map[id.ID]Chapter
	Scenes               map[id.ID]Scene
	Beats                map[id.ID]Beat
	Characters           map[id.ID]CharacterState
	Locations            map[id.ID]LocationState
	Relationships        map[id.ID]Relationship
	Stats                map[string]Stat
	Facts                map[id.ID]Fact
	Knowledge            map[string]Knowledge
	Beliefs              map[id.ID]Belief
	WorldSystems         map[string]WorldSystem
	WorldRules           map[string]WorldRule
	WorldResources       map[string]WorldResourceState
	Items                map[id.ID]ItemState
	Threads              map[id.ID]StoryThread
	DirectorInstructions map[id.ID]DirectorInstruction
}

func NewState(timelineID id.ID) State {
	return State{TimelineID: timelineID, Chapters: map[id.ID]Chapter{}, Scenes: map[id.ID]Scene{}, Beats: map[id.ID]Beat{}, Characters: map[id.ID]CharacterState{}, Locations: map[id.ID]LocationState{}, Relationships: map[id.ID]Relationship{}, Stats: map[string]Stat{}, Facts: map[id.ID]Fact{}, Knowledge: map[string]Knowledge{}, Beliefs: map[id.ID]Belief{}, WorldSystems: map[string]WorldSystem{}, WorldRules: map[string]WorldRule{}, WorldResources: map[string]WorldResourceState{}, Items: map[id.ID]ItemState{}, Threads: map[id.ID]StoryThread{}, DirectorInstructions: map[id.ID]DirectorInstruction{}}
}
func statKey(s Stat) string           { return string(s.OwnerType) + "|" + s.OwnerID.String() + "|" + s.Key }
func knowledgeKey(k Knowledge) string { return k.FactID.String() + "|" + k.KnowerCharacterID.String() }
func worldResourceKey(v WorldResourceState) string {
	return v.ResourceID + "|" + v.OwnerType + "|" + v.OwnerKey
}

func (s *State) EnsureWorldMaps() {
	if s.WorldSystems == nil {
		s.WorldSystems = map[string]WorldSystem{}
	}
	if s.WorldRules == nil {
		s.WorldRules = map[string]WorldRule{}
	}
	if s.WorldResources == nil {
		s.WorldResources = map[string]WorldResourceState{}
	}
}

func (s *State) PutWorldSystem(v WorldSystem) error {
	if v.TimelineID != s.TimelineID {
		return ErrTimelineMismatch
	}
	s.EnsureWorldMaps()
	s.WorldSystems[v.ID] = v
	return nil
}
func (s *State) PutWorldRule(v WorldRule) error {
	if v.TimelineID != s.TimelineID {
		return ErrTimelineMismatch
	}
	s.EnsureWorldMaps()
	s.WorldRules[v.ID] = v
	return nil
}
func (s *State) PutWorldResource(v WorldResourceState) error {
	if v.TimelineID != s.TimelineID {
		return ErrTimelineMismatch
	}
	s.EnsureWorldMaps()
	s.WorldResources[worldResourceKey(v)] = v
	return nil
}
func (s *State) PutRelationship(r Relationship) error {
	if r.TimelineID != s.TimelineID {
		return ErrTimelineMismatch
	}
	if _, ok := s.Relationships[r.ID]; ok {
		return ErrDuplicate
	}
	s.Relationships[r.ID] = r
	return nil
}
func (s *State) PutStat(v Stat) error {
	if v.TimelineID != s.TimelineID {
		return ErrTimelineMismatch
	}
	s.Stats[statKey(v)] = v
	return nil
}
func (s *State) PutFact(v Fact) error {
	if v.TimelineID != s.TimelineID {
		return ErrTimelineMismatch
	}
	if _, ok := s.Facts[v.ID]; ok {
		return ErrDuplicate
	}
	s.Facts[v.ID] = v
	return nil
}
func (s *State) GrantKnowledge(v Knowledge) error {
	if v.TimelineID != s.TimelineID {
		return ErrTimelineMismatch
	}
	f, ok := s.Facts[v.FactID]
	if !ok || f.Status != FactActive {
		return ErrNotFound
	}
	s.Knowledge[knowledgeKey(v)] = v
	return nil
}
func (s *State) PutBelief(v Belief) error {
	if v.TimelineID != s.TimelineID {
		return ErrTimelineMismatch
	}
	s.Beliefs[v.ID] = v
	return nil
}
func (s *State) PutItem(v ItemState) error {
	if v.TimelineID != s.TimelineID {
		return ErrTimelineMismatch
	}
	s.Items[v.ItemID] = v
	return nil
}

// CloneForTimeline creates an independent branch projection. Every Timeline-scoped
// value is rebound to the child Timeline so later mutations cannot alias parent state.
func (s State) CloneForTimeline(target id.ID) State {
	out := NewState(target)
	for k, v := range s.Chapters {
		v.TimelineID = target
		out.Chapters[k] = v
	}
	for k, v := range s.Scenes {
		out.Scenes[k] = v
	}
	for k, v := range s.Beats {
		out.Beats[k] = v
	}
	for k, v := range s.Characters {
		v.TimelineID = target
		out.Characters[k] = v
	}
	for k, v := range s.Locations {
		v.TimelineID = target
		out.Locations[k] = v
	}
	for k, v := range s.Relationships {
		v.TimelineID = target
		out.Relationships[k] = v
	}
	for k, v := range s.Stats {
		v.TimelineID = target
		out.Stats[k] = v
	}
	for k, v := range s.Facts {
		v.TimelineID = target
		out.Facts[k] = v
	}
	for k, v := range s.Knowledge {
		v.TimelineID = target
		out.Knowledge[k] = v
	}
	for k, v := range s.Beliefs {
		v.TimelineID = target
		out.Beliefs[k] = v
	}
	for k, v := range s.WorldSystems {
		v.TimelineID = target
		out.WorldSystems[k] = v
	}
	for k, v := range s.WorldRules {
		v.TimelineID = target
		out.WorldRules[k] = v
	}
	for k, v := range s.WorldResources {
		v.TimelineID = target
		out.WorldResources[k] = v
	}
	for k, v := range s.Items {
		v.TimelineID = target
		out.Items[k] = v
	}
	for k, v := range s.Threads {
		v.TimelineID = target
		out.Threads[k] = v
	}
	for k, v := range s.DirectorInstructions {
		v.TimelineID = target
		out.DirectorInstructions[k] = v
	}
	return out
}

type IDGenerator func() (id.ID, error)

// ForkForTimeline remaps Timeline-local entity IDs while retaining Story-global
// identities such as CharacterID and ItemID. This makes a child projection
// independently materializable under globally unique PostgreSQL primary keys.
func (s State) ForkForTimeline(target id.ID, newID IDGenerator) (State, error) {
	out := NewState(target)
	chapterIDs := map[id.ID]id.ID{}
	sceneIDs := map[id.ID]id.ID{}
	relationshipIDs := map[id.ID]id.ID{}
	factIDs := map[id.ID]id.ID{}

	for oldID, v := range s.Chapters {
		fresh, err := newID()
		if err != nil {
			return State{}, err
		}
		chapterIDs[oldID] = fresh
		v.ID = fresh
		v.TimelineID = target
		out.Chapters[fresh] = v
	}
	for oldID, v := range s.Scenes {
		fresh, err := newID()
		if err != nil {
			return State{}, err
		}
		sceneIDs[oldID] = fresh
		v.ID = fresh
		if mapped, ok := chapterIDs[v.ChapterID]; ok {
			v.ChapterID = mapped
		}
		out.Scenes[fresh] = v
	}
	for _, v := range s.Beats {
		fresh, err := newID()
		if err != nil {
			return State{}, err
		}
		v.ID = fresh
		if mapped, ok := sceneIDs[v.SceneID]; ok {
			v.SceneID = mapped
		}
		out.Beats[fresh] = v
	}
	for k, v := range s.Characters {
		v.TimelineID = target
		out.Characters[k] = v
	}
	for k, v := range s.Locations {
		v.TimelineID = target
		out.Locations[k] = v
	}
	for oldID, v := range s.Relationships {
		fresh, err := newID()
		if err != nil {
			return State{}, err
		}
		relationshipIDs[oldID] = fresh
		v.ID = fresh
		v.TimelineID = target
		out.Relationships[fresh] = v
	}
	for _, v := range s.Stats {
		v.TimelineID = target
		if v.OwnerType == OwnerRelationship {
			if mapped, ok := relationshipIDs[v.OwnerID]; ok {
				v.OwnerID = mapped
			}
		}
		if v.OwnerType == OwnerScene {
			if mapped, ok := sceneIDs[v.OwnerID]; ok {
				v.OwnerID = mapped
			}
		}
		out.Stats[statKey(v)] = v
	}
	for oldID, v := range s.Facts {
		fresh, err := newID()
		if err != nil {
			return State{}, err
		}
		factIDs[oldID] = fresh
		v.ID = fresh
		v.TimelineID = target
		out.Facts[fresh] = v
	}
	for _, v := range s.Knowledge {
		v.TimelineID = target
		if mapped, ok := factIDs[v.FactID]; ok {
			v.FactID = mapped
		}
		out.Knowledge[knowledgeKey(v)] = v
	}
	for _, v := range s.Beliefs {
		fresh, err := newID()
		if err != nil {
			return State{}, err
		}
		v.ID = fresh
		v.TimelineID = target
		out.Beliefs[fresh] = v
	}
	for k, v := range s.WorldSystems {
		v.TimelineID = target
		out.WorldSystems[k] = v
	}
	for k, v := range s.WorldRules {
		v.TimelineID = target
		out.WorldRules[k] = v
	}
	for k, v := range s.WorldResources {
		v.TimelineID = target
		out.WorldResources[k] = v
	}
	for k, v := range s.Items {
		v.TimelineID = target
		out.Items[k] = v
	}
	for _, v := range s.Threads {
		fresh, err := newID()
		if err != nil {
			return State{}, err
		}
		v.ID = fresh
		v.TimelineID = target
		out.Threads[fresh] = v
	}
	for _, v := range s.DirectorInstructions {
		fresh, err := newID()
		if err != nil {
			return State{}, err
		}
		v.ID = fresh
		v.TimelineID = target
		out.DirectorInstructions[fresh] = v
	}
	return out, nil
}
