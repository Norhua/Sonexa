<script setup lang="ts">
import { computed } from 'vue'
import InputText from '@/volt/InputText.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import { editValue,valueOf,type EditValue } from '@/lib/useEditDraft'
const props=defineProps<{field:string;label:string;value?:EditValue}>()
const emit=defineEmits<{change:[value:EditValue]}>()
const current=computed(()=>valueOf(props.value))
function change(value:unknown){emit('change',editValue(value))}
function text(e:Event){return (e.target as HTMLInputElement).value}
function roles(){return [...(current.value??[])]}
function role(index:number,key:string,value:string){const list=roles();list[index]={...list[index],...(key==='name'?{artist:{...list[index].artist,identity:list[index].artist?.name===value?list[index].artist?.identity:undefined,name:value}}:{attributes:value.split('、').filter(Boolean)})};change(list)}
function medium(index:number,value:string){const list=structuredClone(current.value??[]);list[index].trackTotal=value===''?{state:'unknown'}:{state:'value',value:Number(value)};change(list)}
</script>
<template>
 <div v-if="field.includes('.roles.')" class="space-y-2">
  <div v-for="(member,i) in (current??[]) as any[]" :key="i" class="flex flex-wrap gap-1">
   <InputText :model-value="member.artist?.name??''" :aria-label="`${label}姓名 ${i+1}`" class="min-w-0 flex-1" @update:model-value="role(i,'name',$event??'')" />
   <InputText :model-value="member.attributes?.join('、')??''" :aria-label="`${label}属性 ${i+1}`" placeholder="如合声、客串（顿号分隔）" class="min-w-0 flex-1" @update:model-value="role(i,'attributes',$event??'')" />
   <button :aria-label="`移除${label} ${i+1}`" @click="change(roles().filter((_,index)=>index!==i))">移除</button>
  </div>
  <SecondaryButton size="small" @click="change([...roles(),{artist:{name:''},role:field.split('.').at(-1),attributes:[]}])">添加{{ label }}</SecondaryButton>
 </div>
 <div v-else-if="field==='release.media'" class="space-y-1">
  <label v-for="(disc,i) in (current??[]) as any[]" :key="i" class="flex items-center gap-2">第 {{ disc.number }} 碟
   <input type="number" min="1" step="1" :aria-label="`第${disc.number}碟曲数`" :value="disc.trackTotal?.state==='value'?disc.trackTotal.value:''" placeholder="未知曲数" class="w-24 rounded border p-1" @input="medium(i,text($event))" />
   <button :aria-label="`移除第${disc.number}碟`" @click="change(current.filter((_:unknown,index:number)=>index!==i).map((disc:any,index:number)=>({...disc,number:index+1})))">移除</button>
  </label>
  <SecondaryButton size="small" @click="change([...(current??[]),{number:(current?.length??0)+1,trackTotal:{state:'unknown'}}])">添加一碟</SecondaryButton>
 </div>
 <textarea v-else-if="field==='track.lyrics'" :aria-label="label" :value="current??''" rows="5" class="w-full rounded border p-2" @input="change(text($event)||null)" />
 <select v-else-if="field==='release.primaryType'" :aria-label="label" :value="current?.toLowerCase()??''" class="w-full rounded border p-2" @change="change(text($event)||null)">
  <option value="">未知</option><option value="album">专辑</option><option value="single">单曲</option><option value="ep">EP</option><option value="broadcast">广播</option><option value="other">其他</option>
 </select>
 <InputText v-else-if="field==='release.secondaryTypes'" :aria-label="label" :model-value="current?.join('、')??''" placeholder="以顿号分隔" class="w-full" @update:model-value="change(($event??'').split('、').filter(Boolean))" />
 <input v-else-if="['track.year','track.discNumber','track.trackNumber','release.discTotal'].includes(field)" type="number" min="1" step="1" :aria-label="label" :value="current??''" placeholder="未知" class="w-full rounded border p-2" @input="change(text($event)===''?null:Number(text($event)))" />
 <InputText v-else :aria-label="label" :model-value="current??''" class="w-full" @update:model-value="change($event||null)" />
</template>
