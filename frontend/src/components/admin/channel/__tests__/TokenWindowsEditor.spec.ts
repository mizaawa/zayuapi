import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import TokenWindowsEditor from '../TokenWindowsEditor.vue'
import { emptyWindow, validateTokenWindows, type TokenWindow } from '@/utils/tokenPriceWindows'
import { tokenPricingIntervals } from '@/components/modelPlaza/plazaPricing'
const root = defineComponent({ components: { TokenWindowsEditor }, setup() { return { windows: ref<TokenWindow<string | number | null>[]>([]) } }, template: '<TokenWindowsEditor :windows="windows" @update="windows = $event" />' })
describe('Shanghai token pricing', () => {
  it('starts empty and round trips four clock inputs, six prices, and explicit zero', async () => {
    const wrapper = mount(root, { global: { plugins: [createI18n({legacy:false,missingWarn:false,fallbackWarn:false})] } })
    expect(wrapper.find('fieldset').exists()).toBe(false)
    await wrapper.get('button').trigger('click')
    const clocks = wrapper.findAll('input[type="text"]'); expect(clocks).toHaveLength(4)
    for (const [i,v] of ['23','30','2','15'].entries()) { await clocks[i].setValue(v); await clocks[i].trigger('blur') }
    expect(wrapper.findAll('input[type="number"]')).toHaveLength(6)
    await wrapper.get('[data-price="input_price"]').setValue('0')
    expect(wrapper.vm.windows[0]).toMatchObject({ start:'23:30', end:'02:15', input_price:'0' })
    const card = {...emptyWindow(),input_price:2,output_price:4,time_windows:wrapper.vm.windows}
    expect(validateTokenWindows(card)).toBeNull()
    expect(validateTokenWindows({...card,time_windows:[...card.time_windows,{...card.time_windows[0],start:'01:00',end:'06:00'}]})).toBe('overlappingWindows')
    expect(validateTokenWindows({...card, time_windows:[{...card.time_windows[0], start:'24:00'}]})).toBe('invalidWindow')
    expect(validateTokenWindows({...card,output_price:null})).toBe('fallbackRequired')
    await wrapper.get('fieldset button').trigger('click'); expect(wrapper.vm.windows).toHaveLength(0)
  })
  it('shows inherited and explicitly free window prices without applying another card or multiplier', () => {
    const card = {...emptyWindow(),billing_mode:'token_peak' as const,input_price:2,output_price:4,image_output_price:6,time_windows:[{...emptyWindow(),start:'22:00',end:'03:00',input_price:0,image_output_price:1}]}
    const rows=tokenPricingIntervals(card, 'Fallback')
    expect(rows[0]).toMatchObject({tier_label:'Fallback',input_price:2,image_output_price:6});expect(rows[1]).toMatchObject({tier_label:'22:00–03:00 (UTC+8)',input_price:0,output_price:4,image_output_price:1})
  })
})
