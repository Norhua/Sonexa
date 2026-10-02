import { describe,it,expect } from 'vitest'
import { editValue,useEditDraft } from '../../src/lib/useEditDraft'
import type { Models } from '../../src/api'
const session=()=>({trackId:1,releaseId:2,current:{'track.title':editValue('旧'),'release.title':editValue('专辑')},upstream:{},revisions:{'track:1':1}} as unknown as Models.domain.EditorSession)
describe('编辑草稿',()=>{
 it('预览不改草稿，忽略迟到响应',()=>{const d=useEditDraft();d.open(session());const old=d.requestOwner(),latest=d.requestOwner();const p={id:'p',fields:{'track.title':editValue('新')}} as unknown as Models.domain.EditPreview;expect(d.acceptPreview(old,p)).toBe(false);expect(d.acceptPreview(latest,p)).toBe(true);expect(d.dirty.value).toBe(false);expect(d.fields.value['track.title'].valueJson).toBe('"旧"')})
 it('使用一次覆盖手工值，未知保留，明确空清空',()=>{const d=useEditDraft();d.open(session());d.set('track.title','手工');const p={id:'p',fields:{'track.title':editValue(null),'release.title':{state:'unknown'}}} as unknown as Models.domain.EditPreview;d.acceptPreview(d.requestOwner(),p);d.usePreview(p);expect(d.fields.value['track.title'].state).toBe('empty');expect(d.fields.value['release.title'].valueJson).toBe('"专辑"');expect(d.draft().bindingAction).toBe('set')})
 it('角色、文件和选择参与草稿，重新预览保留输入',()=>{const d=useEditDraft();d.open(session());d.set('track.roles.vocal',[{artist:{name:'初音未来'},attributes:['合声']}]);d.coverAction.value='clear';d.mappings.value=[{trackId:1,selected:true,discNumber:null,trackNumber:null} as unknown as Models.domain.EditMapping];d.rebase(session());expect(d.draft().mappings[0]?.selected).toBe(true);expect(d.draft().coverAction).toBe('clear');expect(d.changes.value['track.roles.vocal']).toBeTruthy();expect(d.dirty.value).toBe(true)})
})
