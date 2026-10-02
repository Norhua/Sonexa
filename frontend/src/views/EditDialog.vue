<script setup lang="ts">
import { computed,onMounted,onBeforeUnmount,ref,watch } from 'vue'
import { CircleAlert,ArrowLeft,RotateCcw,ArrowRight } from 'lucide-vue-next'
import { App,type Models } from '@/api'
import { useSettingsStore } from '@/stores/settings'
import { useAdmissionStore } from '@/stores/admission'
import { useEditDraft,valueOf,copy,editableKeys } from '@/lib/useEditDraft'
import EditFieldInput from '@/components/EditFieldInput.vue'
import CoverArt from '@/components/CoverArt.vue'
import Dialog from '@/volt/Dialog.vue'
import Button from '@/volt/Button.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import InputText from '@/volt/InputText.vue'
import Select from '@/volt/Select.vue'
const props=defineProps<{trackId:number}>()
const emit=defineEmits<{close:[];saved:[]}>()
const settings=useSettingsStore(),admission=useAdmissionStore(),edit=useEditDraft()
const impactPending=ref(false)
const loading=ref(false),error=ref(''),source=ref(''),region=ref(''),searchTitle=ref(''),searchArtist=ref(''),searchAlbum=ref(''),trackRef=ref(''),releaseRef=ref('')
const versions=ref<Models.app.EditVersion[]>([]),versionTotal=ref<number|null>(null)
const candidates=ref<Models.app.CandidateRow[]>([]),showTracklist=ref(false)
const useConfirmation=ref<Models.domain.EditPreview|null>(null),useValues=ref(true),albumPrompt=ref(false),impactOpen=ref(false),exitPrompt=ref(false)
const albumPlan=ref<Models.domain.AlbumChangePlan|null>(null),focusId=ref(0),coverURL=ref('')
let alive=true,impactGeneration=0
const tokens=new Set<string>()
const labels:Record<string,string>={'track.title':'曲名','track.credit':'歌曲署名','track.year':'年份','track.discNumber':'碟号','track.trackNumber':'音轨号','track.genre':'风格','track.lyrics':'歌词','track.roles.vocal':'演唱者','track.roles.composer':'作曲','track.roles.lyricist':'作词','release.title':'专辑名','release.credit':'专辑艺术家','release.primaryType':'类型','release.secondaryTypes':'其他类型','release.discTotal':'碟数','release.media':'每碟曲数'}
const groups=[{label:'歌曲信息',keys:editableKeys.filter(k=>k.startsWith('track.'))},{label:'专辑信息',keys:editableKeys.filter(k=>k.startsWith('release.'))}]
const expectedTrackRef=computed(()=>edit.bindingAction.value==='clear_track'||edit.bindingAction.value==='clear_release'?'':edit.bindingPreview.value?.trackIdentity?.ref??edit.baseSession.value?.trackIdentity?.ref??'')
const expectedReleaseRef=computed(()=>edit.bindingAction.value==='clear_release'?'':edit.bindingPreview.value?.releaseIdentity?.ref??edit.baseSession.value?.releaseIdentity?.ref??'')
const rawBindingDirty=computed(()=>!!edit.baseSession.value&&(trackRef.value!==expectedTrackRef.value||releaseRef.value!==expectedReleaseRef.value))
const selectedRow=computed(()=>albumPlan.value?.rows.find(r=>r.trackId===focusId.value))
const snapshot=computed(()=>edit.preview.value?JSON.parse(edit.preview.value.snapshotJson):null)
const readonlyFields=computed(()=>{const fields={...edit.upstream.value};if(edit.bindingAction.value!=='clear_release'){const data=JSON.parse(edit.bindingPreview.value?.snapshotJson??edit.baseSession.value?.snapshotJson??'{}');const date=data.group?.firstReleaseDate;if(date)fields['group.firstReleaseDate']={state:date.state,valueJson:JSON.stringify(date.value),mode:'follow',source:''} as Models.domain.EditValue}return fields})
const tracklist=computed(()=>snapshot.value?.release?.tracklist?.value??[])
const capabilities=computed(()=>settings.providerById(source.value)?.capabilities??[])
const factLabels:Record<string,string>={durationMS:'时长（毫秒）',recording:'录音标识',works:'作品',identifiers:'行业标识',country:'国家',date:'发行日期',status:'发行状态',labels:'厂牌与目录号',barcode:'条码',releaseTrackTotal:'发行总曲数',tracklist:'发行清单',artwork:'上游封面',firstReleaseDate:'首次发行日期',trackNumberText:'上游音轨号',disambiguation:'消歧说明',arranger:'编曲',producer:'制作',mixer:'混音',conductor:'指挥',instrument:'器乐演奏',performer:'演出'}
function display(value:unknown):string{if(value===null||value===undefined)return '未知';if(Array.isArray(value))return value.map(display).join('、')||'空列表';if(typeof value==='object'){const v=value as Record<string,any>;if(v.state)return v.state==='value'?display(v.value):v.state==='empty'?'已清空':'未知';if(v.kind)return `${v.kind}：${display(v.value)}`;if(v.ref)return `${v.provider??''} ${v.ref}`;if(v.artist)return `${v.artist.name}${v.attributes?.length?'（'+v.attributes.join('、')+'）':''}`;if(v.number)return `第${v.number}碟 ${v.trackTotal?.value??'未知'}首`;return v.text??v.name??(v.identity?display(v.identity):'已提供')}return String(value)}
function fieldText(field?:Models.domain.EditValue){return field?.state==='empty'?'已清空':display(valueOf(field))}
async function run(action:()=>Promise<void>){loading.value=true;error.value='';try{await action()}catch(e){if(alive)error.value=String(e)}finally{if(alive)loading.value=false}}
onMounted(()=>run(async()=>{const session=await App.GetEditSession(props.trackId);if(!alive)return;edit.open(session.editor!);source.value=session.editor!.trackIdentity?.provider??settings.providers[0]?.id??'';trackRef.value=session.editor!.trackIdentity?.ref??'';releaseRef.value=session.editor!.releaseIdentity?.ref??'';searchTitle.value=valueOf(session.editor!.current['track.title'])??'';searchArtist.value=valueOf(session.editor!.current['track.credit'])??'';searchAlbum.value=valueOf(session.editor!.current['release.title'])??'';const owner=edit.requestOwner();const preview=await App.PreviewCurrentEditUpstream(props.trackId);if(preview&&alive)edit.acceptPreview(owner,preview)}))
onBeforeUnmount(()=>{alive=false;edit.invalidateRequests();impactGeneration++;for(const token of tokens)void App.DiscardEditCover(token)})
watch(source,()=>{edit.invalidateRequests();candidates.value=[];versions.value=[];versionTotal.value=null;edit.preview.value=null},{flush:'sync'})
function close(){if(loading.value)return;if(edit.dirty.value||rawBindingDirty.value)exitPrompt.value=true;else emit('close')}
function confirmUse(fill=true){if(!edit.preview.value)return;useValues.value=fill;useConfirmation.value=copy(edit.preview.value)}
async function applyUse(){if(!useConfirmation.value)return;const frozen=copy(useConfirmation.value);await run(async()=>{let cover:Models.app.EditCoverDraft|null=null;const artwork=frozen.fields['release.artwork'];if(useValues.value&&artwork?.state==='value')cover=await App.StageEditUpstreamCover(frozen.id);if(useConfirmation.value?.id!==frozen.id){if(cover)await App.DiscardEditCover(cover.token);return}edit.usePreview(frozen,useValues.value);if(cover){tokens.add(cover.token);edit.coverToken.value=cover.token;edit.coverAction.value='replace';coverURL.value=cover.previewUrl}else if(useValues.value&&artwork?.state==='empty'){edit.coverAction.value='clear';edit.coverToken.value=''}trackRef.value=frozen.trackIdentity?.ref??'';releaseRef.value=frozen.releaseIdentity?.ref??'';useConfirmation.value=null})}
async function search(){const owner=edit.requestOwner();await run(async()=>{const rows=await App.SearchEditCandidates(props.trackId,source.value,searchTitle.value,searchArtist.value,searchAlbum.value,region.value);if(alive&&edit.ownsRequest(owner))candidates.value=rows??[]})}
async function choose(row:Models.app.CandidateRow){const owner=edit.requestOwner();await run(async()=>{const preview=await App.PreviewEditCandidate(row);if(alive)edit.acceptPreview(owner,preview)})}
async function fetchPreview(manual=false){const current=edit.preview.value;const owner=edit.requestOwner();await run(async()=>{const preview=await App.LoadEditUpstream(manual?source.value:current?.releaseIdentity?.provider??source.value,manual?releaseRef.value:current?.releaseIdentity?.ref??'',manual?trackRef.value:current?.trackIdentity?.ref??'',region.value,true);if(alive)edit.acceptPreview(owner,preview)})}
async function listVersions(){const owner=edit.requestOwner();await run(async()=>{const group=snapshot.value?.group?.identity??snapshot.value?.release?.group;if(!group)throw new Error('上游未提供发行组身份');const page=await App.ListEditVersions(group.provider,group.ref,versions.value.length);if(alive&&edit.ownsRequest(owner)){versions.value.push(...page.items);versionTotal.value=page.total??null}})}
async function chooseVersion(ref:string){const owner=edit.requestOwner();await run(async()=>{const preview=await App.LoadEditUpstream(edit.preview.value?.releaseIdentity?.provider??source.value,ref,'',region.value,false);if(alive&&edit.acceptPreview(owner,preview))showTracklist.value=true})}
async function chooseTrack(ref:string){const owner=edit.requestOwner();await run(async()=>{const preview=await App.LoadEditUpstream(edit.preview.value?.releaseIdentity?.provider??source.value,edit.preview.value?.releaseIdentity?.ref??'',ref,region.value,false);if(alive)edit.acceptPreview(owner,preview)})}
async function pickCover(){await run(async()=>{const picked=await App.PickEditCover();if(!picked)return;tokens.add(picked.token);edit.coverToken.value=picked.token;edit.coverAction.value='replace';coverURL.value=picked.previewUrl})}
async function refreshImpact(){const owner=++impactGeneration;impactPending.value=true;const draft=edit.draft();try{const plan=await App.PlanAlbumChange(draft);if(owner!==impactGeneration||!alive)return;albumPlan.value=plan;if(!focusId.value)focusId.value=plan.rows[0]?.trackId??0}catch(e){if(owner===impactGeneration){error.value=String(e);albumPlan.value=null}}finally{if(owner===impactGeneration)impactPending.value=false}}
async function openImpact(){albumPrompt.value=false;impactOpen.value=true;await run(refreshImpact)}
function mapping(id:number){return edit.mappings.value.find(m=>m.trackId===id)}
function updateMapping(id:number,patch:Partial<Models.domain.EditMapping>){const row=albumPlan.value?.rows.find(r=>r.trackId===id);if(!row)return;const existing=mapping(id)??{trackId:id,selected:false,targetRef:row.choices.length===1?row.choices[0]!.ref:'',discNumber:valueOf(row.before['track.discNumber']),trackNumber:valueOf(row.before['track.trackNumber'])} as Models.domain.EditMapping;edit.mappings.value=[...edit.mappings.value.filter(m=>m.trackId!==id),{...existing,...patch}];void refreshImpact()}
watch(()=>JSON.stringify([edit.fields.value,edit.bindingAction.value,edit.coverAction.value,edit.coverToken.value]),()=>{albumPlan.value=null;if(impactOpen.value)void refreshImpact()})
async function save(){exitPrompt.value=false;if(rawBindingDirty.value){error.value='绑定输入尚未确认，请先解析绑定并选择使用或仅更换绑定';return}await run(async()=>{const impact=await App.PreviewEditSave(edit.draft());if(impact.sharedChange){albumPrompt.value=true;return}await commit()})}
async function commit(){const saved=await admission.run(()=>App.PlanEditSave(edit.draft()),App.SaveEdit);if(saved){emit('saved');emit('close')}}
async function confirmImpact(){if(!albumPlan.value?.canSubmit)return;await run(commit)}
function clearBinding(release=false){edit.clearBinding(release);trackRef.value='';if(release)releaseRef.value=''}
async function rebase(){await run(async()=>{const session=await App.GetEditSession(props.trackId);edit.rebase(session.editor!);const binding=edit.bindingPreview.value;if(edit.bindingAction.value==='set'&&binding){const next=await App.LoadEditUpstream(binding.trackIdentity?.provider??source.value,binding.releaseIdentity?.ref??'',binding.trackIdentity?.ref??'',region.value,true);edit.refreshBinding(next)}if(impactOpen.value)await refreshImpact()})}
</script>
<template>
 <Dialog :visible="true" modal :closable="!loading" header="编辑曲目" :dismissable-mask="false" :style="{width:'min(1280px,96vw)',height:'min(860px,92vh)'}" @update:visible="close">
  <div v-if="edit.baseSession.value" class="grid min-h-0 gap-5 md:grid-cols-[42%_1fr]">
   <section class="space-y-3 md:max-h-[65vh] md:overflow-y-auto md:pr-3" aria-label="上游信息">
    <h2 class="font-semibold">上游信息</h2>
    <Select v-model="source" :options="settings.providers" option-label="displayName" option-value="id" aria-label="上游来源" class="w-full" />
    <InputText v-if="settings.providerById(source)?.needsCountry" v-model="region" aria-label="地区" placeholder="地区" />
    <div class="grid gap-2"><InputText v-model="searchTitle" aria-label="搜索曲名" placeholder="曲名"/><InputText v-model="searchArtist" aria-label="搜索艺术家" placeholder="艺术家"/><InputText v-model="searchAlbum" aria-label="搜索专辑" placeholder="专辑"/></div>
    <SecondaryButton :disabled="loading||!settings.outboundAllowed" @click="search">搜索候选</SecondaryButton>
    <p v-if="!settings.outboundAllowed" class="text-sm text-muted-color">{{settings.outboundReason}}；仍可离线编辑。</p>
    <div class="space-y-1"><button v-for="(row,i) in candidates" :key="i" class="block w-full rounded border p-2 text-left" @click="choose(row)"><span>{{row.title||'未命名歌曲'}} · {{row.artist||'未知署名'}}</span><span class="block text-xs text-muted-color">{{row.album}}<template v-if="capabilities.includes('Country')"> · {{row.country||'未知国家'}}</template><template v-if="capabilities.includes('MediumFormat')"> · {{row.mediumFormat||'未知载体'}}</template></span></button></div>
    <template v-if="edit.preview.value">
     <h3 class="font-medium">{{fieldText(edit.preview.value.fields['track.title'])}}</h3>
     <p>{{fieldText(edit.preview.value.fields['release.title'])}} · {{fieldText(edit.preview.value.fields['release.credit'])}}</p>
     <SecondaryButton :disabled="loading||!settings.outboundAllowed" @click="fetchPreview()">重新获取预览</SecondaryButton>
     <SecondaryButton v-if="settings.providerById(source)?.supportsVersions" :disabled="loading||!settings.outboundAllowed||(versionTotal!==null&&versions.length>=versionTotal)" @click="listVersions">{{versions.length?'更多版本':'浏览版本'}}</SecondaryButton>
     <button v-for="version in versions" :key="version.ref" class="block w-full rounded border p-2 text-left text-sm" @click="chooseVersion(version.ref)">{{version.title||'未命名发行'}} · {{version.country||'未知国家'}} · {{version.trackTotal||'未知'}}首</button>
     <button class="block text-primary" @click="showTracklist=!showTracklist">{{showTracklist?'收起':'展开'}}当前发行清单</button>
     <div v-if="showTracklist"><p v-if="!tracklist.length" class="text-sm text-muted-color">清单未提供，请重新获取预览。</p><button v-for="(row,i) in tracklist" :key="i" class="block w-full py-1 text-left text-sm" :disabled="!row.track?.value?.ref" @click="chooseTrack(row.track.value.ref)">{{row.discNumber}}-{{row.trackNumber}} {{row.title?.value||'未命名歌曲'}} · {{edit.preview.value.trackStates?.[row.discNumber+':'+row.trackNumber]||'未收藏'}}</button></div>
    </template>
   </section>
   <section class="space-y-4 md:max-h-[65vh] md:overflow-y-auto" aria-label="当前信息">
    <div class="space-y-2 rounded border p-3"><div class="flex items-center justify-between"><h2 class="font-semibold">当前信息</h2><Button :disabled="!edit.preview.value?.trackIdentity||loading" @click="confirmUse()">使用</Button></div>
     <InputText v-model="trackRef" aria-label="歌曲绑定" :placeholder="settings.providerById(source)?.refs?.trackHint" class="w-full"/>
     <InputText v-model="releaseRef" aria-label="发行绑定" :placeholder="settings.providerById(source)?.refs?.releaseHint" class="w-full"/>
     <div class="flex flex-wrap gap-2"><SecondaryButton size="small" :disabled="loading||!settings.outboundAllowed" @click="fetchPreview(true)">解析绑定并预览</SecondaryButton><SecondaryButton size="small" :disabled="!edit.preview.value" @click="confirmUse(false)">仅更换绑定</SecondaryButton><SecondaryButton size="small" @click="clearBinding()">清除歌曲绑定</SecondaryButton><SecondaryButton size="small" @click="clearBinding(true)">解除发行绑定</SecondaryButton></div>
     <p class="text-xs text-muted-color">当前发行其他 {{edit.baseSession.value.otherReleaseTracks}} 首 · 同组其他发行 {{edit.baseSession.value.otherGroupTracks}} 首。填写绑定后请先解析并确认；绑定修改在保存后生效。</p>
    </div>
    <section v-for="group in groups" :key="group.label" class="space-y-3"><h3 class="font-semibold">{{group.label}}</h3>
     <div class="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] gap-2 text-xs text-muted-color"><span>当前值</span><span>上游值</span><span aria-hidden="true"/></div>
     <div v-for="key in group.keys" :key="key" class="space-y-1"><label class="text-sm font-medium">{{labels[key]}}</label><div class="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] items-start gap-2">
      <EditFieldInput :field="key" :label="labels[key]!" :value="edit.fields.value[key]" @change="edit.fields.value[key]=$event"/>
      <p class="whitespace-pre-wrap break-words text-sm text-muted-color">{{fieldText(edit.upstream.value[key])}}<span v-if="edit.upstream.value[key]?.state!=='unknown'&&edit.upstream.value[key]&&fieldText(edit.fields.value[key])===fieldText(edit.upstream.value[key])" class="block text-xs">= 相同</span></p>
      <div class="flex flex-col gap-1"><button :aria-label="`应用${labels[key]}`" :disabled="!edit.upstream.value[key]||edit.upstream.value[key]?.state==='unknown'" @click="edit.fields.value[key]=copy(edit.upstream.value[key]!)"><ArrowLeft class="size-4"/></button><button :aria-label="`撤销${labels[key]}`" @click="edit.undo(key)"><RotateCcw class="size-4"/></button></div>
     </div><p v-if="key==='track.roles.vocal'" class="text-xs text-muted-color">演唱者与歌曲署名独立；未知不表示无人演唱，已有名单不保证完整，可补录演唱者与合声属性。</p></div>
    </section>
    <section class="space-y-2"><h3 class="font-medium">封面</h3><img v-if="coverURL&&edit.coverAction.value==='replace'" :src="coverURL" alt="封面草稿" class="size-24 object-contain"/><CoverArt usage="detail" v-else-if="edit.coverAction.value!=='clear'&&valueOf(edit.baseSession.value.current['release.cover'])?.imageId" :source="{releaseId:edit.baseSession.value.releaseId}" class="size-24"/><p v-else>保存后清除封面</p><div class="flex gap-2"><SecondaryButton size="small" @click="pickCover">选择本地封面</SecondaryButton><SecondaryButton size="small" @click="edit.coverAction.value='clear'">清除封面</SecondaryButton><SecondaryButton size="small" @click="edit.coverAction.value='keep';edit.coverToken.value=''">撤销封面修改</SecondaryButton></div></section>
    <details><summary>其他上游事实（只读）</summary><p v-for="(field,key) in readonlyFields" :key="key" v-show="!editableKeys.includes(String(key))" class="py-1 text-sm">{{factLabels[String(key).split('.').at(-1)!]||'其他事实'}}：{{fieldText(field)}}</p></details>
   </section>
  </div>
  <template #footer><div v-if="error" role="alert" class="mr-auto text-sm text-danger-color">{{error}} <button @click="rebase">重新预览</button></div><SecondaryButton :disabled="loading" @click="close">取消</SecondaryButton><Button :disabled="loading||!edit.baseSession.value" @click="save">保存</Button></template>
 </Dialog>
 <Dialog :visible="!!useConfirmation" modal :header="useValues?'使用当前上游信息':'更换绑定'" :style="{width:'min(520px,94vw)'}" @update:visible="useConfirmation=null"><p>{{useValues?'将把当前上游提供的歌曲和专辑信息全部填入当前编辑值，并替换已有的手工修改。未提供的信息将保留；上游明确为空的字段将清空。':'仅替换绑定，保留当前编辑值。'}}修改只在点击保存后写入数据库。</p><template #footer><SecondaryButton @click="useConfirmation=null">取消</SecondaryButton><Button :disabled="loading" @click="applyUse">确认使用</Button></template></Dialog>
 <Dialog v-model:visible="albumPrompt" modal header="专辑信息有改动" :style="{width:'min(460px,94vw)'}"><p>新建专辑并逐曲确认迁入，原发行及未勾选歌曲保持原状。精确上游目标已在库时复用。</p><template #footer><SecondaryButton @click="albumPrompt=false">取消</SecondaryButton><Button @click="openImpact">新建专辑</Button></template></Dialog>
 <Dialog v-model:visible="impactOpen" :closable="!loading" :close-on-escape="!loading" modal header="确认迁入歌曲" :dismissable-mask="false" :style="{width:'min(1100px,96vw)'}">
  <p v-if="albumPlan?.impact?.preservesTargetValues" class="mb-3">复用在库发行，保留目标当前专辑信息。</p>
  <div v-if="albumPlan" class="grid gap-5 md:grid-cols-[42%_1fr]">
   <section aria-label="原发行全部收藏" class="max-h-[60vh] space-y-1 overflow-y-auto"><button v-for="row in albumPlan.rows" :key="row.trackId" :aria-label="`查看${fieldText(row.before['track.title'])}`" :aria-current="row.trackId===focusId?'true':undefined" class="flex w-full items-start gap-2 rounded border p-3 text-left" :class="row.stay?'text-muted-color':'text-green-600 dark:text-green-400'" @click="focusId=row.trackId"><span class="flex-1">{{fieldText(row.before['track.title'])}}<span class="block text-xs">{{fieldText(row.before['track.credit'])}} · {{fieldText(row.before['track.discNumber'])}}-{{fieldText(row.before['track.trackNumber'])}} · {{row.stay?'未确认迁入':'已确认迁入'}}</span></span><CircleAlert v-if="row.conflict" :aria-label="row.conflict" :title="row.conflict" class="size-5 shrink-0 text-danger-color"/></button></section>
   <section v-if="selectedRow" aria-label="歌曲迁入方案" class="max-h-[60vh] space-y-3 overflow-y-auto">
    <label class="flex gap-2 font-medium"><input type="checkbox" :checked="!!mapping(selectedRow.trackId)?.selected" @change="updateMapping(selectedRow.trackId,{selected:($event.target as HTMLInputElement).checked})"/>将此曲加入新建专辑</label>
    <template v-if="albumPlan.impact?.linksNewUpstream"><label>目标清单对应<select class="mt-1 w-full rounded border p-2" :value="mapping(selectedRow.trackId)?.targetRef??''" @change="updateMapping(selectedRow.trackId,{targetRef:($event.target as HTMLSelectElement).value})"><option value="">请选择对应</option><option v-for="target in albumPlan.targets" :key="target.ref" :value="target.ref">{{target.discNumber}}-{{target.trackNumber}} {{target.title}}</option></select></label><p class="text-xs">碟号和音轨号采用所选清单位置。</p></template>
    <div v-else class="flex gap-3"><label v-for="(label,key) in {discNumber:'碟号',trackNumber:'音轨号'}" :key="key">{{label}}<input type="number" min="1" step="1" :aria-label="`迁入${label}`" class="mt-1 block w-24 rounded border p-2" :value="(mapping(selectedRow.trackId)?mapping(selectedRow.trackId)![key]:valueOf(selectedRow.before['track.'+key]))??''" placeholder="未知" @input="updateMapping(selectedRow.trackId,{[key]:($event.target as HTMLInputElement).value===''?null:Number(($event.target as HTMLInputElement).value)})"/></label></div>
    <p v-if="selectedRow.conflict" role="alert" class="text-danger-color">{{selectedRow.conflict}}</p><p>{{selectedRow.evidence}}</p>
    <p class="break-all text-xs">歌曲身份：{{selectedRow.beforeIdentity?.ref||'本地'}} → {{selectedRow.afterIdentity?.ref||'本地'}}</p>
    <div v-for="key in editableKeys" :key="key" class="grid grid-cols-[5rem_1fr_auto_1fr] gap-2 border-b py-2 text-sm"><span>{{labels[key]}}</span><span class="break-words">{{fieldText(selectedRow.before[key])}}</span><ArrowRight class="size-4"/><span class="break-words">{{fieldText(selectedRow.after[key])}}</span></div>
   </section>
  </div><p v-else>正在准备影响方案…</p>
  <template #footer><p v-if="error" role="alert" class="mr-auto text-sm text-danger-color">{{error}}</p><SecondaryButton :disabled="loading" @click="impactOpen=false">取消</SecondaryButton><Button :disabled="loading||impactPending||!albumPlan?.canSubmit" @click="confirmImpact">确认影响并继续</Button></template>
 </Dialog>
 <Dialog v-model:visible="exitPrompt" modal header="有未保存的修改" :style="{width:'min(480px,94vw)'}"><p>是否保存本次编辑？</p><template #footer><SecondaryButton @click="exitPrompt=false">继续编辑</SecondaryButton><SecondaryButton @click="emit('close')">不保存</SecondaryButton><Button @click="save">保存</Button></template></Dialog>
</template>
