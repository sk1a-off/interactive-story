const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? ''

export function createActionRequestId(): string {
  if (typeof globalThis.crypto?.randomUUID === 'function') {
    return globalThis.crypto.randomUUID()
  }

  // crypto.randomUUID() is unavailable in some browsers on plain HTTP origins
  // (for example http://<wireguard-ip>:5173). getRandomValues() is still
  // available there, so build a standards-compliant UUID v4 ourselves.
  if (typeof globalThis.crypto?.getRandomValues === 'function') {
    const bytes = new Uint8Array(16)
    globalThis.crypto.getRandomValues(bytes)
    bytes[6] = (bytes[6] & 0x0f) | 0x40
    bytes[8] = (bytes[8] & 0x3f) | 0x80

    const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
    return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
  }

  // Very old WebViews may expose neither method. The backend only requires a
  // unique idempotency key, not a cryptographic credential.
  return `action-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

async function json<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${apiBaseUrl}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) }
  })
  if (!response.ok) {
    const raw = await response.text()
    let message = raw || `Request failed: ${response.status}`
    if (raw) {
      try {
        const payload = JSON.parse(raw) as { message?: string; code?: string }
        message = payload.message?.trim() || payload.code?.trim() || message
      } catch {
        // Keep the original response body when it is not JSON.
      }
    }
    throw new Error(message)
  }
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}
export async function getHealth(signal?: AbortSignal): Promise<{ status: string }> {
  return json('/health/ready', { signal })
}

export type NarrationHealth = { status:string; model?:string; device?:string; gpu?:string; numStep?:number; voicePrompt?:string }
export const getNarrationHealth=(signal?:AbortSignal)=>json<NarrationHealth>('/api/v1/narration/health',{signal})
export async function synthesizeNarration(text:string,signal?:AbortSignal):Promise<Blob>{
  const response=await fetch(`${apiBaseUrl}/api/v1/narration/speech`,{method:'POST',signal,headers:{'Content-Type':'application/json'},body:JSON.stringify({text})})
  if(!response.ok){
    const raw=await response.text();let message=raw||`Request failed: ${response.status}`
    try{const payload=JSON.parse(raw) as {message?:string;code?:string};message=payload.message?.trim()||payload.code?.trim()||message}catch{/* Keep upstream text. */}
    throw new Error(message)
  }
  return response.blob()
}
export type SetupComponent = { storyId:string; key:string; revision:number; source:string; payload:Record<string,unknown>; locked:boolean; status:string }
export type Story = { ID:string; Title:string; Description:string }
export type StoryListItem = {
  id:string; title:string; description:string; status:string;
  readyComponents:number; totalComponents:number;
  latestTimeline?:{id:string;name:string;status:string;headEventSeq:number};
  createdAt:string; updatedAt:string;
}
export type StartResult = { timeline:{ ID:string; Name:string }; sceneId:string; beatId:string; imageGenerationId?:string }
export type SceneBeat={id:string;position:number;text:string;paragraphs?:string[]}; export type SceneImage={id:string;variant:number;url:string}; export type SceneImageGeneration={id:string;sceneId:string;sourceBeatId?:string;momentIndex:number;anchorParagraph?:number;status:'pending'|'running'|'done'|'failed';images:SceneImage[];selectedImageId?:string}; export type ChoiceSet={beatId:string;choices:string[]}
export type Objective={id:string;parentObjectiveId?:string;scope:'global'|'minor';kind:'quest'|'task'|'event'|'milestone';questType?:'main'|'side';title:string;description?:string;successCriteria:string;status:'active'|'completed'|'failed';progress:number;evidence?:string}
export type JournalEntry={id:string;category:'ability'|'attribute'|'item'|'currency';name:string;description?:string;quantity?:number;level?:string;status:'active'|'inactive';evidence?:string;tags?:string[]}
export type HeroStat={key:string;valueType:string;value:unknown;name?:string}
export type Current = { timelineId:string; storyId:string; headEventSeq:number; chapterNumber:number; chapterTitle:string; chapterGoal:string; sceneId:string; sceneNumber:number; sceneGoal:string; beatId:string; text:string; beats?:SceneBeat[]; choices:string[]; choiceSet?:ChoiceSet; objectives:Objective[]; abilities?:JournalEntry[]; attributes?:JournalEntry[]; inventory?:JournalEntry[]; currencies?:JournalEntry[]; heroStats?:HeroStat[]; imageGeneration?:SceneImageGeneration; imageGenerations?:SceneImageGeneration[] }
export const createStory=(body:{title:string;idea:string;tags:string[]})=>json<Story>('/api/v1/stories',{method:'POST',body:JSON.stringify(body)})
export const listStories=()=>json<{stories:StoryListItem[]}>('/api/v1/stories')
export const deleteStory=(storyId:string)=>json<void>(`/api/v1/stories/${storyId}`,{method:'DELETE'})
export const generateSetup=(storyId:string,components?:string[])=>json<{components:SetupComponent[]}>(`/api/v1/stories/${storyId}/setup/generate`,{method:'POST',body:JSON.stringify({components:components??[]})})
export const getSetup=(storyId:string)=>json<{components:SetupComponent[]}>(`/api/v1/stories/${storyId}/setup`)
export const setSetupLock=(storyId:string,key:string,locked:boolean)=>json<SetupComponent>(`/api/v1/stories/${storyId}/setup/components/${key}/lock`,{method:'PUT',body:JSON.stringify({locked})})
export const editSetup=(storyId:string,key:string,payload:Record<string,unknown>,locked:boolean)=>json<SetupComponent>(`/api/v1/stories/${storyId}/setup`,{method:'PATCH',body:JSON.stringify({componentKey:key,payload,locked})})
export const regenerateSetup=(storyId:string,key:string)=>json<{components:SetupComponent[]}>(`/api/v1/stories/${storyId}/setup/regenerate`,{method:'POST',body:JSON.stringify({components:[key]})})
export type SetupAssistTarget={path?:string;matchField?:string;matchValue?:string;childPath?:string;childMatchField?:string;childMatchValue?:string}
export type SetupAssistOperation={component:string;operation:'set_field'|'remove_field'|'add_item'|'update_item'|'remove_item'|'add_child_item'|'update_child_item'|'remove_child_item';path:string;matchField?:string;matchValue?:string;childPath?:string;childMatchField?:string;childMatchValue?:string;value?:unknown}
export type SetupAssistChange={key:string;before:Record<string,unknown>;after:Record<string,unknown>;changedPaths:string[];operations:SetupAssistOperation[]}
export type SetupAssistResult={generationId:string;summary:string;changes:SetupAssistChange[]}
export const assistSetup=(storyId:string,body:{instruction:string;components:string[];drafts:Record<string,Record<string,unknown>>;action?:string;target?:SetupAssistTarget})=>json<SetupAssistResult>(`/api/v1/stories/${storyId}/setup/assist`,{method:'POST',body:JSON.stringify(body)})
export const startStory=(storyId:string)=>json<StartResult>(`/api/v1/stories/${storyId}/start`,{method:'POST',body:'{}'})
export const getCurrent=(timelineId:string)=>json<Current>(`/api/v1/timelines/${timelineId}/current`)

export const submitAction=(timelineId:string,expectedHead:number,text:string,requestId=createActionRequestId())=>json<{generationId:string}>(`/api/v1/timelines/${timelineId}/actions`,{method:'POST',headers:{'Idempotency-Key':requestId},body:JSON.stringify({expectedHead,text})})
export const generationEventsUrl=(generationId:string,after=0)=>`${apiBaseUrl}/api/v1/generations/${generationId}/events${after>0?`?after=${after}`:''}`

export type DirectorView = {
  timelineId:string; semanticRevision:number; headEventSeq:number;
  characters:Array<Record<string,unknown>>; locations:Array<Record<string,unknown>>; relationships:Array<Record<string,unknown>>;
  stats:Array<Record<string,unknown>>; items:Array<Record<string,unknown>>;
  facts:Array<Record<string,unknown>>; knowledge:Array<Record<string,unknown>>;
  beliefs:Array<Record<string,unknown>>; threads:Array<Record<string,unknown>>;
  objectives:Array<Record<string,unknown>>;
  abilities:Array<Record<string,unknown>>; attributes:Array<Record<string,unknown>>; inventory:Array<Record<string,unknown>>;
  worldSystems:Array<Record<string,unknown>>; worldRules:Array<Record<string,unknown>>; worldResources:Array<Record<string,unknown>>; worldRuleAudit:Array<Record<string,unknown>>;
  instructions:Array<Record<string,unknown>>;
}
export const getDirector=(timelineId:string)=>json<DirectorView>(`/api/v1/timelines/${timelineId}/director`)
export const applyDirectorExact=(timelineId:string,expectedRevision:number,type:string,payload:Record<string,unknown>,note?:string)=>json(`/api/v1/timelines/${timelineId}/director/exact`,{method:'POST',body:JSON.stringify({expectedRevision,type,payload,note})})
export const addDirectorInstruction=(timelineId:string,text:string,scope:string,priority:string)=>json(`/api/v1/timelines/${timelineId}/director/instructions`,{method:'POST',body:JSON.stringify({text,scope,priority})})
export const getDirectorHistory=(timelineId:string)=>json<{entries:Array<Record<string,unknown>>}>(`/api/v1/timelines/${timelineId}/director/history`)
export type DirectorAssistOperation={type:'upsert_character'|'archive_character'|'upsert_location'|'archive_location'|'upsert_objective'|'archive_objective'|'upsert_world_system'|'upsert_world_rule'|'archive_world_rule'|'update_world_resource';reference?:string;parentReference?:string;payload:Record<string,unknown>;note?:string}
export type DirectorAssistResult={generationId:string;summary:string;operations:DirectorAssistOperation[]}
export const assistDirector=(timelineId:string,body:{instruction:string;section:string;selectedId?:string})=>json<DirectorAssistResult>(`/api/v1/timelines/${timelineId}/director/assist`,{method:'POST',body:JSON.stringify(body)})

export type TimelineCard={id:string;storyId:string;name:string;status:string;parentTimelineId?:string;forkedFromSaveId?:string;headEventSeq:number}
export type SaveCard={id:string;timelineId:string;name:string;note:string;kind:string;pinned:boolean;eventSeq:number;beatId?:string;createdAt:string;displaySlot?:number;thumbnailStatus:string;thumbnailAssetId?:string}
export type SaveLibrary={timelines:TimelineCard[];saves:Record<string,SaveCard[]>}
export const getSaveLibrary=(storyId:string)=>json<SaveLibrary>(`/api/v1/stories/${storyId}/save-library`)
export const createSave=(timelineId:string,name:string,note:string,pinned=false)=>json<SaveCard>(`/api/v1/timelines/${timelineId}/saves`,{method:'POST',body:JSON.stringify({name,note,pinned})})
export const updateSaveCard=(saveId:string,displaySlot:number|undefined,pinned:boolean)=>json<void>(`/api/v1/saves/${saveId}`,{method:'PATCH',body:JSON.stringify({displaySlot,pinned})})
export const previewSave=(saveId:string)=>json<{save:SaveCard;eventSeq:number;stateHash:string;state:unknown}>(`/api/v1/saves/${saveId}/restore`,{method:'POST',body:JSON.stringify({mode:'preview'})})
export const forkSave=(saveId:string,newTimelineName:string)=>json<TimelineCard>(`/api/v1/saves/${saveId}/restore`,{method:'POST',body:JSON.stringify({mode:'fork',newTimelineName})})

export type ProviderIdentity={kind:string;provider:string;model:string;profile:string}
export type AIConfigView={id:string;revision:number;storyLlm:ProviderIdentity;embedding:ProviderIdentity;image:ProviderIdentity;public:{storyLlmEndpoint:string;storyLlmContext:number;storyLlmKvDType:string;storyLlmSafeHybrid:boolean;worldRulesSafeMode:boolean;embeddingEndpoint:string;imageEndpoint:string};active:boolean;storyLlmSecretConfigured:boolean;storyLlmSecretCount:number}
export const listAISettings=()=>json<{revisions:AIConfigView[]}>('/api/v1/settings/ai')
export const createAIRevision=(body:{config:{storyLlm:ProviderIdentity;embedding:ProviderIdentity;image:ProviderIdentity;public:AIConfigView['public']};storyLlmApiKey?:string;storyLlmApiKeys?:string[];activate:boolean})=>json<AIConfigView>('/api/v1/settings/ai/revisions',{method:'POST',body:JSON.stringify(body)})
export const activateAIRevision=(id:string)=>json<AIConfigView>(`/api/v1/settings/ai/revisions/${id}/activate`,{method:'POST',body:'{}'})


export type ImageGenerationView={id:string;sceneId:string;status:'pending'|'running'|'done'|'failed';attempts:number;errorCode?:string;images:SceneImage[];selectedImageId?:string}
export const generateSceneImages=(storyId:string,sceneId:string)=>json<ImageGenerationView>(`/api/v1/stories/${storyId}/scenes/${sceneId}/images`,{method:'POST',body:'{}'})
export const generateParagraphImage=(storyId:string,sceneId:string,beatId:string,paragraph:number)=>json<ImageGenerationView>(`/api/v1/stories/${storyId}/scenes/${sceneId}/images/paragraph`,{method:'POST',body:JSON.stringify({beatId,paragraph})})
export const getImageGeneration=(generationId:string)=>json<ImageGenerationView>(`/api/v1/image-generations/${generationId}`)

export type PromptRoleSettings={temperature:number;topP:number;maxTokens:number}
export type PromptSetView={id:string;revision:number;prompts:Record<string,string>;roleSettings:Record<string,PromptRoleSettings>;active:boolean}
export type PromptStudioView={active:PromptSetView;revisions:PromptSetView[];builtin:{prompts:Record<string,string>;roleSettings:Record<string,PromptRoleSettings>}}
export const getPromptStudio=()=>json<PromptStudioView>('/api/v1/settings/prompts')
export const createPromptRevision=(body:{prompts:Record<string,string>;roleSettings:Record<string,PromptRoleSettings>;activate:boolean})=>json<PromptSetView>('/api/v1/settings/prompts/revisions',{method:'POST',body:JSON.stringify(body)})
export const activatePromptRevision=(id:string)=>json<PromptSetView>(`/api/v1/settings/prompts/revisions/${id}/activate`,{method:'POST',body:'{}'})
