import { computed, ref } from 'vue'
import type { Models } from '@/api'
export type EditValue = Models.domain.EditValue
export type EditFields = Record<string, EditValue>
export const editableKeys = ['track.title','track.credit','track.year','track.discNumber','track.trackNumber','track.genre','track.lyrics','track.roles.vocal','track.roles.composer','track.roles.lyricist','release.title','release.credit','release.primaryType','release.secondaryTypes','release.discTotal','release.media']
export function copy<T>(value: T): T { return JSON.parse(JSON.stringify(value)) }
export function valueOf(field?: EditValue): any { return field?.state === 'value' ? JSON.parse(field.valueJson) : null }
export function editValue(value: unknown): EditValue { return { state: value === null ? 'empty' : 'value', valueJson: value === null ? '' : JSON.stringify(value), mode: 'user', source: 'user' } as EditValue }
function same(a?: EditValue,b?: EditValue) { return a?.state === b?.state && JSON.stringify(valueOf(a)) === JSON.stringify(valueOf(b)) }
export function useEditDraft() {
 const baseSession = ref<Models.domain.EditorSession | null>(null)
 const preview = ref<Models.domain.EditPreview | null>(null)
 const bindingPreview = ref<Models.domain.EditPreview | null>(null)
 const upstream = ref<EditFields>({})
 const fields = ref<EditFields>({})
 const bindingAction = ref('keep'), previewId = ref(''), coverAction = ref('keep'), coverToken = ref('')
 const mappings = ref<Models.domain.EditMapping[]>([])
 const albumAction = ref<'create' | 'update'>('create')
 let generation = 0
 const changes = computed(() => Object.fromEntries(Object.entries(fields.value).filter(([key,v]) => !same(v,baseSession.value?.current[key]))))
 const dirty = computed(() => Object.keys(changes.value).length>0 || bindingAction.value!=='keep' || coverAction.value!=='keep' || mappings.value.length>0)
 function open(session: Models.domain.EditorSession) {
  generation++; baseSession.value=copy(session);fields.value=copy(session.current);upstream.value=copy(session.upstream)
  preview.value=null;bindingPreview.value=null;bindingAction.value='keep';previewId.value='';coverAction.value='keep';coverToken.value='';mappings.value=[];albumAction.value='create'
 }
 function requestOwner() { return ++generation }
 function acceptPreview(owner:number,next:Models.domain.EditPreview) { if(owner!==generation)return false;preview.value=copy(next);return true }
 function ownsRequest(owner:number){return owner===generation}
 function invalidateRequests(){generation++}
 function set(key:string,value:unknown){fields.value[key]=editValue(value)}
 function undo(key:string){if(baseSession.value?.current[key])fields.value[key]=copy(baseSession.value.current[key]);else delete fields.value[key]}
 function usePreview(frozen:Models.domain.EditPreview,fill=true){
  if(preview.value?.id!==frozen.id)throw new Error('预览已改变，请重新确认')
  bindingAction.value='set';refreshBinding(frozen)
  if(fill)for(const key of editableKeys){const value=frozen.fields[key];if(value&&value.state!=='unknown')fields.value[key]=copy(value)}
  mappings.value=[]
 }
 function refreshBinding(next:Models.domain.EditPreview){bindingPreview.value=copy(next);previewId.value=next.id;upstream.value=copy(next.fields)}
 function clearBinding(release=false){bindingAction.value=release?'clear_release':'clear_track';previewId.value='';bindingPreview.value=null;upstream.value=release?{}:Object.fromEntries(Object.entries(upstream.value).filter(([k])=>k.startsWith('release.')));mappings.value=[]}
 function draft():Models.domain.EditDraft{
  if(!baseSession.value)throw new Error('编辑会话未就绪')
  return {trackId:baseSession.value.trackId,revisions:copy(baseSession.value.revisions),fields:copy(changes.value),bindingAction:bindingAction.value,previewId:previewId.value,coverAction:coverAction.value,coverToken:coverToken.value,albumAction:albumAction.value,mappings:copy(mappings.value),mergeTargetId:0} as Models.domain.EditDraft
 }
 function rebase(session:Models.domain.EditorSession){const retained=copy(changes.value);baseSession.value=copy(session);fields.value={...copy(session.current),...retained}}
 return {baseSession,preview,bindingPreview,refreshBinding,upstream,fields,bindingAction,previewId,coverAction,coverToken,albumAction,mappings,changes,dirty,open,requestOwner,acceptPreview,ownsRequest,invalidateRequests,set,undo,usePreview,clearBinding,draft,rebase}
}
