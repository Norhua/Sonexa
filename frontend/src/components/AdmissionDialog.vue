<script setup lang="ts">
import { computed } from 'vue'
import { display } from '@/api'
import { useAdmissionStore } from '@/stores/admission'
import Dialog from '@/volt/Dialog.vue'
import Button from '@/volt/Button.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import Message from '@/volt/Message.vue'
const admission = useAdmissionStore()
const visible = computed({ get: () => !!admission.plan, set: value => { if (!value) admission.cancel() } })
const release = computed(() => admission.plan?.seed.snapshot.release)
</script>
<template>
  <Dialog v-model:visible="visible" modal :closable="!admission.busy" :dismissable-mask="false"
    :header="admission.plan?.baseline.conflict ? '归组证据冲突' : '确认发行成员变化'" :style="{ width: 'min(760px, 94vw)' }">
    <div v-if="admission.plan" class="space-y-4 text-sm">
      <p>{{ admission.plan.baseline.conflict ? '证据命中多个专辑组。请选择合组后的目标组，检查成员变化后再确认；也可取消并修正资料。' : '本次将新增发行、身份映射或合并专辑组。确认前不会改变曲库或复核条目。' }}</p>
      <div class="rounded border p-3">
        <h3 class="font-medium">{{ admission.plan.seed.moveReleaseId ? '拟迁入发行' : '拟加入发行' }}</h3>
        <p>{{ display(release?.title?.value) }} · {{ display(release?.credit?.value?.text) }}</p>
        <p class="break-all text-xs text-muted-color">{{ release?.identity?.provider || '本地' }} · {{ display(release?.identity?.ref) }}</p>
        <p v-if="admission.plan.seed.mergeTargetId" class="mt-1">合并至专辑组 #{{ admission.plan.seed.mergeTargetId }}</p>
      </div>
      <section v-for="group in admission.plan.baseline.resolution.groups" :key="group.id" class="rounded border p-3">
        <h3 class="font-medium">{{ group.title || '未命名专辑组' }} · {{ display(group.credit) }} <span class="text-xs">#{{ group.id }}</span></h3>
        <ul class="mt-2 space-y-2">
          <li v-for="member in group.members" :key="member.id">
            {{ member.title || '未命名发行' }} · {{ display(member.credit) }}
            <p class="break-all text-xs text-muted-color">{{ member.identity?.provider || '本地' }} · {{ display(member.identity?.ref) }}</p>
            <p class="text-xs text-muted-color">受影响收藏 {{ member.trackIds?.length || 0 }} 首<span v-if="member.trackIds?.length">（{{ member.trackIds.join('、') }}）</span></p>
          </li>
        </ul>
        <SecondaryButton v-if="admission.plan.baseline.conflict" class="mt-2" size="small" :disabled="admission.busy" @click="admission.merge(group.id)">预览合并到此组</SecondaryButton>
      </section>
      <section>
        <h3 class="font-medium">判组依据</h3>
        <p v-for="(evidence, i) in admission.plan.baseline.resolution.evidence" :key="i" class="break-all text-xs text-muted-color">
          组 #{{ evidence.groupId }} · {{ evidence.kind === 'native' ? '上游组身份' : '专辑名与专辑艺术家' }} · {{ evidence.title }} · {{ evidence.credit }}<span v-if="evidence.native"> · {{ evidence.native.provider }}:{{ evidence.native.ref }}</span>
        </p>
      </section>
      <Message v-if="admission.error" severity="error">{{ admission.error }}。保留原选择，请重新预览后确认。</Message>
    </div>
    <template #footer>
      <SecondaryButton :disabled="admission.busy" @click="admission.cancel()">取消</SecondaryButton>
      <SecondaryButton v-if="admission.error" :disabled="admission.busy" @click="admission.refresh()">重新预览</SecondaryButton>
      <Button v-else :disabled="admission.busy || admission.plan?.baseline.conflict" @click="admission.confirm()">确认成员变化并保存</Button>
    </template>
  </Dialog>
</template>
