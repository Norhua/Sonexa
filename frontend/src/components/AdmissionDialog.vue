<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { display } from '@/api'
import { useAdmissionStore } from '@/stores/admission'
import {
  TYPED_CONFIRM_WORD, admissionHeader, admissionIntro, admissionMode, evidenceLabel, formatDuration, groupActions,
  providerPosition, providerReleases, rejectedText, typedWordMatches, undecidableReason,
} from '@/lib/admissionPlan'
import Dialog from '@/volt/Dialog.vue'
import Button from '@/volt/Button.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import InputText from '@/volt/InputText.vue'
import Message from '@/volt/Message.vue'
const admission = useAdmissionStore()
const visible = computed({ get: () => !!admission.plan, set: value => { if (!value) admission.cancel() } })
const plan = computed(() => admission.plan)
const baseline = computed(() => plan.value?.baseline)
const release = computed(() => plan.value?.seed.snapshot.release)
const mode = computed(() => admissionMode(baseline.value))
const providers = computed(() => providerReleases(baseline.value))
const actions = computed(() => groupActions(plan.value))
const typed = computed(() => !!baseline.value?.needsTypedConfirm)
// 编辑器保存与整发行换组不能在这里改用其他发行：那是另一种操作。
const canUseProvider = computed(() => !plan.value?.seed.isEdit && !plan.value?.seed.moveReleaseId)
const targetName = computed(() => release.value?.title?.value || '未命名发行')
const keepLabel = computed(() => baseline.value?.existingReleaseId ? `仍加入发行「${targetName.value}」` : `创建发行「${targetName.value}」`)
// 输入确认词的区域默认收起：主动作是使用已有发行。没有可用的提供者时直接展开。
const typing = ref(false)
const word = ref('')
const showTyping = computed(() => typed.value && (typing.value || !providers.value.some(p => p.usable) || !canUseProvider.value))
watch(() => plan.value?.id, () => { typing.value = false; word.value = '' })
</script>
<template>
  <Dialog v-model:visible="visible" modal :closable="!admission.busy" :dismissable-mask="false"
    :header="admissionHeader(mode)" :style="{ width: 'min(760px, 94vw)' }">
    <div v-if="plan && baseline" class="space-y-4 text-sm" data-testid="admission-dialog">
      <p>{{ admissionIntro(mode) }}</p>
      <div class="rounded border p-3">
        <h3 class="font-medium">{{ plan.seed.moveReleaseId ? '拟迁入发行' : baseline.existingReleaseId ? '所选发行' : '拟加入发行' }}</h3>
        <p class="break-words">{{ display(release?.title?.value) }} · {{ display(release?.credit?.value?.text) }}</p>
        <p class="break-all text-xs text-muted-color">{{ release?.identity?.provider || '本地' }} · {{ display(release?.identity?.ref) }}</p>
        <p v-if="plan.seed.mergeTargetId" class="mt-1">合并至专辑组 #{{ plan.seed.mergeTargetId }}</p>
        <p v-else-if="plan.seed.joinGroupId" class="mt-1">加入专辑组 #{{ plan.seed.joinGroupId }}，不合并其他命中的组</p>
        <p v-else-if="plan.seed.keepSeparate" class="mt-1">保持独立成组，不加入命中的专辑组</p>
      </div>

      <section v-if="providers.length" class="space-y-2" data-testid="admission-providers">
        <h3 class="font-medium">能提供这首歌的已有发行</h3>
        <div v-for="(item, i) in providers" :key="item.releaseId" class="rounded border p-3" data-testid="admission-provider">
          <div class="flex flex-wrap items-start gap-2">
            <div class="min-w-0 flex-1">
              <p class="break-words font-medium">{{ item.title }} · {{ display(item.credit) }}<span v-if="i === 0 && providers.length > 1" class="ml-1 text-xs font-normal text-muted-color">（默认）</span></p>
              <p class="break-all text-xs text-muted-color">{{ item.source }} · {{ display(item.ref) }} · 已收藏 {{ item.trackRefs }} 首<span v-if="item.trackTotal"> / 共 {{ item.trackTotal }} 首</span></p>
            </div>
            <component :is="i === 0 ? Button : SecondaryButton" v-if="canUseProvider" size="small" :disabled="admission.busy || !item.usable"
              data-testid="use-provider" @click="admission.useProvider(item.releaseId)">使用发行「{{ item.title }}」</component>
          </div>
          <ul class="mt-2 space-y-1">
            <li v-for="(entry, j) in item.entries" :key="j" class="text-xs">
              <span v-if="providerPosition(entry)" class="tabular-nums text-muted-color">{{ providerPosition(entry) }} · </span>{{ display(entry.trackTitle) }}<span v-if="formatDuration(entry.durationMs)" class="tabular-nums text-muted-color"> · {{ formatDuration(entry.durationMs) }}</span>
              <span class="ml-1 rounded border px-1">{{ evidenceLabel(entry.evidence) }}</span>
              <span v-if="entry.trackId" class="ml-1 text-muted-color">已有曲目，将直接复用</span>
            </li>
          </ul>
          <p v-if="canUseProvider && !item.usable" class="mt-1 text-xs text-muted-color">这条清单没有曲目标识，无法直接改用。</p>
        </div>
        <p v-if="!canUseProvider" class="text-xs text-muted-color">当前操作不能在这里改用其他发行；如要使用已有发行，请取消后在对应发行中操作。</p>
      </section>

      <section v-if="baseline.undecidable?.length" class="space-y-2" data-testid="admission-undecidable">
        <h3 class="font-medium">无法判定的已有发行</h3>
        <div v-for="item in baseline.undecidable" :key="item.releaseId" class="rounded border p-3">
          <p class="break-words">{{ item.title || '未命名发行' }} · {{ display(item.credit) }}</p>
          <p class="break-all text-xs text-muted-color">{{ item.identity?.provider || '本地' }} · {{ display(item.identity?.ref) }} · {{ undecidableReason(item.reason) }}</p>
        </div>
        <p v-if="admission.fetching" class="text-xs text-muted-color" role="status">正在获取曲目清单…</p>
        <Message v-else-if="admission.coverageError" severity="warn" size="small">获取清单失败：{{ admission.coverageError }}</Message>
        <SecondaryButton v-if="!admission.fetching" size="small" :disabled="admission.busy" data-testid="refresh-coverage" @click="admission.refreshCoverage()">重新获取清单</SecondaryButton>
      </section>

      <section v-for="group in baseline.resolution.groups" :key="group.id" class="rounded border p-3">
        <h3 class="break-words font-medium">{{ group.title || '未命名专辑组' }} · {{ display(group.credit) }} <span class="text-xs">#{{ group.id }}</span></h3>
        <ul class="mt-2 space-y-2">
          <li v-for="member in group.members" :key="member.id">
            <span class="break-words">{{ member.title || '未命名发行' }} · {{ display(member.credit) }}</span>
            <p class="break-all text-xs text-muted-color">{{ member.identity?.provider || '本地' }} · {{ display(member.identity?.ref) }}</p>
            <p class="text-xs text-muted-color">受影响收藏 {{ member.trackIds?.length || 0 }} 首<span v-if="member.trackIds?.length">（{{ member.trackIds.join('、') }}）</span></p>
          </li>
        </ul>
        <div v-if="actions.choose" class="mt-2 flex flex-wrap gap-2">
          <SecondaryButton size="small" :disabled="admission.busy" @click="admission.merge(group.id)">预览合并到此组</SecondaryButton>
          <SecondaryButton size="small" :disabled="admission.busy" @click="admission.join(group.id)">加入此组，不合并其他组</SecondaryButton>
        </div>
      </section>
      <SecondaryButton v-if="actions.separate" size="small" :disabled="admission.busy" data-testid="keep-separate" @click="admission.separate()">保持独立成组</SecondaryButton>

      <section v-if="baseline.resolution.evidence?.length || baseline.resolution.rejected?.length || baseline.resolution.similar?.length">
        <h3 class="font-medium">判组依据</h3>
        <p v-for="(evidence, i) in baseline.resolution.evidence" :key="'e' + i" class="break-all text-xs text-muted-color">
          组 #{{ evidence.groupId }} · {{ evidence.kind === 'native' ? '上游组身份' : evidence.kind === 'existing-release' ? '发行已在库' : '专辑名与专辑艺术家' }}<span v-if="evidence.title"> · {{ evidence.title }} · {{ evidence.credit }}</span><span v-if="evidence.native"> · {{ evidence.native.provider }}:{{ evidence.native.ref }}</span>
        </p>
        <p v-for="(evidence, i) in baseline.resolution.rejected" :key="'r' + i" class="break-all text-xs text-muted-color" data-testid="rejected-evidence">
          {{ rejectedText(evidence, release?.identity?.provider) }} · {{ evidence.title }} · {{ evidence.credit }}
        </p>
        <p v-for="(evidence, i) in baseline.resolution.similar" :key="'s' + i" class="break-all text-xs text-muted-color" data-testid="similar-evidence">
          名称近似的专辑组 #{{ evidence.groupId }} · {{ evidence.title }} · {{ evidence.credit }}（仅差版本后缀或署名附注，未自动并入）
        </p>
      </section>

      <div v-if="showTyping" class="rounded border p-3" data-testid="typed-confirm">
        <label class="block text-sm" for="admission-typed-word">输入 {{ TYPED_CONFIRM_WORD }} 以确认{{ baseline.existingReleaseId ? '仍加入所选发行' : '创建新的发行' }}</label>
        <InputText id="admission-typed-word" v-model="word" class="mt-2 w-full" autocomplete="off" spellcheck="false"
          :disabled="admission.busy" @keydown.enter.prevent="typedWordMatches(word) && admission.confirm(word)" />
      </div>
      <Message v-if="admission.error" severity="error">{{ admission.error }}。保留原选择，请重新预览后确认。</Message>
    </div>
    <template #footer>
      <div class="flex flex-wrap justify-end gap-2">
        <SecondaryButton :disabled="admission.busy" @click="admission.cancel()">取消</SecondaryButton>
        <SecondaryButton v-if="admission.error" :disabled="admission.busy" @click="admission.refresh()">重新预览</SecondaryButton>
        <template v-else-if="typed">
          <SecondaryButton v-if="!showTyping" :disabled="admission.busy" data-testid="keep-target" @click="typing = true">{{ keepLabel }}</SecondaryButton>
          <Button v-else :disabled="admission.busy || !typedWordMatches(word)" data-testid="typed-submit" @click="admission.confirm(word)">{{ keepLabel }}</Button>
        </template>
        <Button v-else :disabled="admission.busy || baseline?.conflict" @click="admission.confirm()">确认成员变化并保存</Button>
      </div>
    </template>
  </Dialog>
</template>
