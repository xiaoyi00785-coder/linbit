package click.cowa.linbit.feature.profile

import click.cowa.linbit.core.generated.*
import click.cowa.linbit.core.network.NetworkFactory
import kotlinx.coroutines.test.runTest
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.*
import org.junit.Test

class ProfileApiTest {
    private val server = MockWebServer()
    private val api = NetworkFactory.api(NetworkFactory.bareClient(), server.url("/").toString())
    @After fun tearDown() { server.shutdown() }

    @Test fun accountKycAndTotpPathsMatchOpenApi() = runTest {
        server.enqueue(MockResponse().setBody("""{"uid":"u1","kycLevel":1,"twoFactorEnabled":false,"createdAt":"2026-01-01"}"""))
        assertEquals("u1", api.account().uid)
        assertEquals("/account", server.takeRequest().path)

        server.enqueue(MockResponse().setBody("""{"level":0,"status":"none"}"""))
        assertEquals(ApiKycStatus.Status.NONE, api.kycStatus().status)
        assertEquals("/kyc/status", server.takeRequest().path)

        server.enqueue(MockResponse().setBody("""{"secret":"ABC","otpauthUri":"otpauth://totp/Linbit:test?secret=ABC"}"""))
        assertEquals("ABC", api.bindTotp().secret)
        assertEquals("POST", server.takeRequest().method)

        server.enqueue(MockResponse().setBody("""{"enabled":true,"method":"totp"}"""))
        assertTrue(api.verifyTotp(ApiVerifyTotpRequest("123456")).enabled)
        assertEquals("{\"code\":\"123456\"}", server.takeRequest().body.readUtf8())

        server.enqueue(MockResponse().setBody("""{"enabled":false}"""))
        assertFalse(api.disableTotp(ApiDisableTotpRequest("123456")).enabled)
        assertEquals("/2fa/totp/disable", server.takeRequest().path)
    }

    @Test fun kycUploadUsesRequiredMultipartFields() = runTest {
        server.enqueue(MockResponse().setResponseCode(201).setBody("""{"fileId":"doc1","side":"front"}"""))
        val file = okhttp3.MultipartBody.Part.createFormData("file", "front.jpg", byteArrayOf(1, 2).toRequestBody("image/jpeg".toMediaType()))
        assertEquals("doc1", api.uploadKycDocument(file, "front".toRequestBody("text/plain".toMediaType())).fileId)
        val request = server.takeRequest()
        val body = request.body.readUtf8()
        assertEquals("/kyc/documents", request.path)
        assertTrue(request.getHeader("Content-Type")?.startsWith("multipart/") == true)
        assertTrue(body.contains("name=\"file\""))
        assertTrue(body.contains("filename=\"front.jpg\""))
        assertTrue(body.contains("name=\"side\""))
        assertTrue(body.contains("front"))
    }

    @Test fun subAccountListAndCreateMatchOpenApi() = runTest {
        server.enqueue(MockResponse().setBody("""{"items":[{"uid":"sub1","username":"strategy"}],"total":1}"""))
        assertEquals("strategy", api.subAccounts().items?.single()?.username)
        assertEquals("/sub-accounts?page=1&size=100", server.takeRequest().path)
        server.enqueue(MockResponse().setBody("""{"uid":"sub1","username":"strategy"}"""))
        api.createSubAccount(ApiSubAccountCreateRequest("strategy"))
        assertEquals("/sub-accounts", server.takeRequest().path)
    }
}
