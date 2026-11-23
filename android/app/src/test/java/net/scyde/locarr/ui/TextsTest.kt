package net.scyde.locarr.ui

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class TextsTest {
    @Test fun placeholders() {
        assertEquals("Downloading Dune: 42% (x).", interpolate("Downloading {{title}}: {{ progress }}% ({{state}}).", mapOf("title" to "Dune", "progress" to "42", "state" to "x")))
        assertEquals("Missing .", interpolate("Missing {{languages}}.", emptyMap()))
    }

    @Test fun jsonParamsAsText() {
        val p = JsonObject(mapOf("languages" to JsonArray(listOf(JsonPrimitive("hr"), JsonPrimitive("en"))), "n" to JsonPrimitive(3)))
        assertEquals(mapOf("languages" to "hr, en", "n" to "3"), p.texts())
    }

    @Test fun generatedCodesCoverTheServersCodeTables() {
        assertTrue(CODE_STRINGS.containsKey("health.codes.indexer_failing"))
        assertTrue(CODE_STRINGS.containsKey("explain.codes.all_rejected"))
        assertTrue(CODE_STRINGS.containsKey("notifications.types.request_available"))
        assertTrue(CODE_STRINGS.containsKey("board.groups.import_failed.title"))
    }

    @Test fun formatting() {
        assertEquals("0 B", formatBytes(0))
        assertEquals("1m 5s", formatDuration(65))
        assertEquals("2h 0m", formatDuration(7200))
    }
}
