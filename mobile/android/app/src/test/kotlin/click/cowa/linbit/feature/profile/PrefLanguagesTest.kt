package click.cowa.linbit.feature.profile

import org.junit.Assert.*
import org.junit.Test

class PrefLanguagesTest {
    @Test fun referenceOrderAndOnlySimplifiedChineseSupported() {
        assertEquals(14, PrefLanguages.all.size)
        assertEquals("English", PrefLanguages.all[0])
        assertEquals("简体中文", PrefLanguages.all[1])
        assertEquals("Français", PrefLanguages.all.last())
        assertEquals("简体中文", PrefLanguages.SUPPORTED)
        assertEquals(1, PrefLanguages.all.count { it == PrefLanguages.SUPPORTED })
    }
}
