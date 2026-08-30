(function process(request, response) {
    'use strict';
    var result = new TopoControlPlane().complete(String(request.pathParams.id || ''), request.body.data || {});
    response.setHeader('Cache-Control', 'no-store');
    response.setStatus(result.status);
    response.setBody(result.body);
})(request, response);
