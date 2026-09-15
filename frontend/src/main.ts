import { createApp } from 'vue'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import App from './App.vue'
import './style.css'

/*
 * PrimeVue 以 unstyled 模式接入：组件只提供行为与无障碍语义，
 * 全部外观由 src/volt/ 下各组件的 pass-through 类名给出。
 * 这正是 Volt 的形态——样式在仓库里、可直接改，而不是藏在主题包中。
 */
createApp(App).use(createPinia()).use(PrimeVue, { unstyled: true }).mount('#app')
