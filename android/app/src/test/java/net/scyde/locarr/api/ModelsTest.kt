package net.scyde.locarr.api

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ModelsTest {
    @Test fun unknownFieldsAndEnumValuesAreTolerated() {
        val text = """
            {"rows":[{"key":"some_new_row","items":[{"mediaType":"podcast","title":"X","year":2030,"status":"brand_new",
              "status4k":"none","futureField":{"a":[1,2]}}]}],"requests":[],"metadataReady":true,"musicReady":false,"extra":1}
        """
        val home = json.decodeFromString(DiscoverHome.serializer(), text)
        assertEquals("some_new_row", home.rows[0].key)
        assertEquals("podcast", home.rows[0].items[0].mediaType)
        assertEquals("brand_new", home.rows[0].items[0].status)
        assertEquals(false, home.musicReady)
    }

    @Test fun missingFieldsGetDefaults() {
        val r = json.decodeFromString(MediaRequest.serializer(), """{"id":7,"title":"Dune"}""")
        assertEquals(7L, r.id)
        assertEquals("pending", r.status)
        assertTrue(r.seasons.isEmpty())
        assertNull(r.explanation)
    }

    @Test fun nullsForNonNullableFieldsFallBackToDefaults() {
        val m = json.decodeFromString(Movie.serializer(), """{"id":1,"title":"A","tags":null,"monitored":null}""")
        assertTrue(m.tags.isEmpty())
        assertEquals(false, m.monitored)
        assertEquals("movie:1", m.ref)
    }

    @Test fun notificationsWithParams() {
        val list = json.decodeFromString(NotificationList.serializer(), """
            {"items":[{"id":12,"type":"request.available","title":"Dune","requestId":3,"params":{"seasons":[1,2]},
              "createdAt":"2026-11-23T10:00:00Z","read":false,"actor":{"id":1,"username":"ana"}}],"unread":1,"latestId":12}
        """)
        assertEquals(12L, list.latestId)
        assertEquals(3L, list.items[0].requestId)
        assertEquals("ana", list.items[0].actor?.username)
    }

    @Test fun interactiveSearchAndQueue() {
        val s = json.decodeFromString(InteractiveSearch.serializer(), """
            {"kind":"movies","profileName":"HD","accepted":1,"pending":0,"rejected":1,"durationMs":40,
             "decisions":[{"decision":{"candidate":{"title":"Dune.2021.1080p","size":100,"protocol":"torrent","indexerId":2,"seeders":5},
               "release":{"input":"x","kind":"movie","title":"Dune","video":{},"audio":{},"revision":{"version":1},"confidence":1},
               "quality":{"id":"bluray-1080p","name":"Bluray-1080p","source":"bluray","music":false,"rank":9},
               "customFormats":[],"score":0,"status":"rejected","rejections":[{"code":"some_future_code","message":"nope"}],
               "rankKey":{}},"downloadToken":"tok"}],
             "indexers":[{"indexerId":2,"indexerName":"Fake","status":"ok","count":1,"cached":false,"durationMs":3}]}
        """)
        assertEquals("some_future_code", s.decisions[0].decision.rejections[0].code)
        assertEquals("tok", s.decisions[0].downloadToken)
        val q = json.decodeFromString(Queue.serializer(), """{"items":[{"id":1,"title":"t","state":"stalledDL_new","progress":0.5,"item":{"ref":"movie:4","kind":"movie","id":4,"title":"Dune"}}],"clients":[]}""")
        assertEquals("stalledDL_new", q.items[0].state)
        assertEquals("movie:4", q.items[0].item?.ref)
    }

    @Test fun seriesDetailFlattensSeasons() {
        val s = json.decodeFromString(Series.serializer(), """{"id":3,"itemRef":"series:3","title":"S","seasons":[{"id":9,"number":1,"monitored":true,"stats":{"episodeCount":10,"episodeFileCount":4}}]}""")
        assertEquals(1, s.seasons.size)
        assertEquals(4, s.seasons[0].stats.episodeFileCount)
    }
}
